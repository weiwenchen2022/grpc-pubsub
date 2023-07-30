/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package main implements a simple gRPC server for pubsub service whose definition can be found in pubsub/pubsub.proto.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"path"
	"sync"
	"time"

	pb "github.com/weiwenchen2022/grpc-pubsub/pubsub"
	"github.com/weiwenchen2022/pubsub"

	"github.com/weiwenchen2022/lockable"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var port = flag.Int("port", 50051, "The server port")

type pubsubServer struct {
	pb.UnimplementedPubsubServer

	p *pubsub.Publisher

	pendingMessage pendingMap
	activeChannels activeMap
	activePatterns activeMap
}

type pendingMap struct {
	*lockable.Lockable[map[*pb.Message]int]
}

func (m *pendingMap) store(key *pb.Message, value int) {
	_ = m.Lockable.Do(false, func(data *map[*pb.Message]int) error {
		(*data)[key] = value
		return nil
	})
}

func (m *pendingMap) incr(key *pb.Message) {
	_ = m.Lockable.Do(false, func(data *map[*pb.Message]int) error {
		(*data)[key]++
		return nil
	})
}

func (m *pendingMap) loadAndDelete(key *pb.Message) int {
	var n int
	_ = m.Lockable.Do(false, func(data *map[*pb.Message]int) error {
		n = (*data)[key]
		delete(*data, key)
		return nil
	})
	return n
}

type activeMap struct {
	*lockable.Lockable[map[string][]chan any]
}

func (m *activeMap) append(channel string, c chan any) {
	_ = m.Lockable.Do(false, func(data *map[string][]chan any) error {
		(*data)[channel] = append((*data)[channel], c)
		return nil
	})
}

func (m *activeMap) remove(channel string, c chan any) error {
	return m.Lockable.Do(false, func(data *map[string][]chan any) error {
		var (
			i  = -1
			ch chan any
		)
		for i, ch = range (*data)[channel] {
			if c == ch {
				break
			}
		}
		if i == -1 {
			return fmt.Errorf("not found channel %q", channel)
		}

		last := len((*data)[channel]) - 1
		if last != i {
			(*data)[channel][i] = (*data)[channel][last]
		}
		(*data)[channel][last] = nil
		if last == 0 {
			delete(*data, channel)
		} else {
			(*data)[channel] = (*data)[channel][:last]
		}
		return nil
	})
}

func (m *activeMap) len() int {
	var n int
	_ = m.Lockable.Do(true, func(data *map[string][]chan any) error {
		n = len(*data)
		return nil
	})
	return n
}

func (m *activeMap) rangeFunc(f func(k string, v []chan any) bool) {
	_ = m.Lockable.Do(true, func(data *map[string][]chan any) error {
		for k, v := range *data {
			if !f(k, v) {
				return nil
			}
		}
		return nil
	})
}

func (s *pubsubServer) filter(f func(*pb.Message) bool) func(any) bool {
	return func(a any) bool {
		if m, ok := a.(*pb.Message); ok && f(m) {
			s.pendingMessage.incr(m)
			return true
		}
		return false
	}
}

func (s *pubsubServer) subscribe(stream pb.Pubsub_SubscribeServer, pendingChannels map[string]chan any, channels ...string) error {
	for _, channel := range channels {
		if _, exists := pendingChannels[channel]; !exists {
			channel := channel
			c, _ := s.p.SubscribeSubject(s.filter(func(m *pb.Message) bool {
				return m.GetChannel() == channel
			}))
			go func() {
				for message := range c {
					if err := stream.Send(message.(*pb.Message)); err != nil {
						return
					}
				}
			}()

			pendingChannels[channel] = c
			s.activeChannels.append(channel, c)
		}

		reply := &pb.Message{
			Type:    "subscribe",
			Channel: channel,
		}
		auxMessage{reply}.SetSubscriptions(int32(len(pendingChannels)))
		if err := stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *pubsubServer) unsubscribe(stream pb.Pubsub_SubscribeServer, pendingChannels map[string]chan any, channels ...string) error {
	for _, channel := range channels {
		c, exists := pendingChannels[channel]
		if !exists {
			continue
		}

		if err := s.activeChannels.remove(channel, c); err != nil {
			return err
		}
		delete(pendingChannels, channel)
		s.p.Unsubscribe(c)

		reply := &pb.Message{
			Type:    "unsubscribe",
			Channel: channel,
		}
		auxMessage{reply}.SetSubscriptions(int32(len(pendingChannels)))
		if err := stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *pubsubServer) Subscribe(stream pb.Pubsub_SubscribeServer) error {
	pendingChannels := make(map[string]chan any)
	for {
		in, err := stream.Recv()
		switch err {
		default:
			return err
		case io.EOF:
			return nil
		case nil:
		}

		switch typ := in.GetType(); typ {
		default:
			return fmt.Errorf("unknown type - %s", typ)
		case "subscribe":
			channels := in.GetChannels()
			if len(channels) == 0 {
				return errors.New("no channels provided")
			}

			if err := s.subscribe(stream, pendingChannels, channels...); err != nil {
				return err
			}
		case "unsubscribe":
			channels := in.GetChannels()
			if len(channels) == 0 {
				channels = make([]string, 0, len(pendingChannels))
				for channel := range pendingChannels {
					channels = append(channels, channel)
				}
			}

			if err := s.unsubscribe(stream, pendingChannels, channels...); err != nil {
				return err
			}

			if len(pendingChannels) == 0 {
				return nil
			}
		}
	}
}

func (s *pubsubServer) Publish(ctx context.Context, request *pb.PublishRequest) (*pb.PublishReply, error) {
	message := &pb.Message{
		Type:    "message",
		Channel: request.GetChannel(),
	}
	auxMessage{message}.SetMessage(request.GetMessage())

	s.pendingMessage.store(message, 0)
	err := s.p.Publish(message)
	received := int32(s.pendingMessage.loadAndDelete(message))

	if err != nil {
		return nil, err
	}
	return &pb.PublishReply{Received: received}, nil
}

func (s *pubsubServer) Channels(ctx context.Context, request *pb.ChannelsRequest) (*pb.ChannelsReply, error) {
	pattern := request.GetPattern()
	if pattern == "" {
		pattern = "*"
	}

	reply := &pb.ChannelsReply{}

	var (
		matched bool
		err     error
	)
	s.activeChannels.rangeFunc(func(channel string, _ []chan any) bool {
		if pattern == "*" {
			reply.Channels = append(reply.Channels, channel)
			return true
		}

		matched, err = path.Match(pattern, channel)
		if err != nil {
			return false
		}
		if matched {
			reply.Channels = append(reply.Channels, channel)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *pubsubServer) NumSub(ctx context.Context, request *pb.NumSubRequest) (*pb.NumSubReply, error) {
	reply := &pb.NumSubReply{}
	_ = s.activeChannels.Do(true, func(data *map[string][]chan any) error {
		for _, channel := range request.GetChannels() {
			channels := (*data)[channel]
			reply.Results = append(reply.Results, &pb.NumSubReply_Result{Channel: channel, Subscribers: int32(len(channels))})
		}
		return nil
	})
	return reply, nil
}

func (s *pubsubServer) psubscribe(stream pb.Pubsub_PSubscribeServer, pendingPatterns map[string]chan any, patterns ...string) error {
	for _, pattern := range patterns {
		if _, exists := pendingPatterns[pattern]; !exists {
			pattern := pattern
			c, _ := s.p.SubscribeSubject(s.filter(func(m *pb.Message) bool {
				matched, _ := path.Match(pattern, m.GetChannel())
				return matched
			}))
			go func() {
				for a := range c {
					message := a.(*pb.Message)
					messagecopy := &pb.Message{
						Type:    "pmessage",
						Pattern: pattern,
						Channel: message.GetChannel(),
					}
					auxMessage{messagecopy}.SetMessage(message.GetMessage())
					if err := stream.Send(messagecopy); err != nil {
						return
					}
				}
			}()

			pendingPatterns[pattern] = c
			s.activePatterns.append(pattern, c)
		}

		reply := &pb.Message{
			Type:    "psubscribe",
			Pattern: pattern,
		}
		auxMessage{reply}.SetSubscriptions(int32(len(pendingPatterns)))
		if err := stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *pubsubServer) punsubscribe(stream pb.Pubsub_PSubscribeServer, pendingPatterns map[string]chan any, patterns ...string) error {
	for _, pattern := range patterns {
		c, existis := pendingPatterns[pattern]
		if !existis {
			continue
		}

		if err := s.activePatterns.remove(pattern, c); err != nil {
			return err
		}
		delete(pendingPatterns, pattern)
		s.p.Unsubscribe(c)

		reply := &pb.Message{
			Type:    "punsubscribe",
			Pattern: pattern,
		}
		auxMessage{reply}.SetSubscriptions(int32(len(pendingPatterns)))
		if err := stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *pubsubServer) PSubscribe(stream pb.Pubsub_PSubscribeServer) error {
	pendingPatterns := make(map[string]chan any)

	for {
		in, err := stream.Recv()
		switch err {
		default:
			return err
		case io.EOF:
			return nil
		case nil:
		}

		switch typ := in.GetType(); typ {
		default:
			return fmt.Errorf("unknown type - %s", typ)
		case "psubscribe":
			patterns := in.GetChannels()
			if len(patterns) == 0 {
				return errors.New("no patterns provided")
			}

			if err := s.psubscribe(stream, pendingPatterns, patterns...); err != nil {
				return err
			}
		case "punsubscribe":
			patterns := in.GetChannels()
			if len(patterns) == 0 {
				patterns = make([]string, 0, len(pendingPatterns))
				for pattern := range pendingPatterns {
					patterns = append(patterns, pattern)
				}
			}

			if err := s.punsubscribe(stream, pendingPatterns, patterns...); err != nil {
				return err
			}

			if len(pendingPatterns) == 0 {
				return nil
			}
		}
	}
}

func (s *pubsubServer) NumPat(ctx context.Context, _ *pb.Null) (*pb.Int64, error) {
	return &pb.Int64{Int64: int64(s.activePatterns.len())}, nil
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterPubsubServer(grpcServer, newServer())

	log.Printf("server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func newServer() *pubsubServer {
	return &pubsubServer{
		p: pubsub.NewPublisher(100*time.Millisecond, 10),

		pendingMessage: pendingMap{lockable.New(new(sync.Mutex), make(map[*pb.Message]int))},

		activeChannels: activeMap{lockable.New(new(sync.RWMutex), make(map[string][]chan any))},
		activePatterns: activeMap{lockable.New(new(sync.RWMutex), make(map[string][]chan any))},
	}
}

type auxMessage struct {
	m *pb.Message
}

var (
	subscriptionsFd = (*pb.Message)(nil).ProtoReflect().Descriptor().Oneofs().ByName(protoreflect.Name("message_oneof")).
			Fields().ByName(protoreflect.Name("subscriptions"))
	messageFd = (*pb.Message)(nil).ProtoReflect().Descriptor().Oneofs().ByName(protoreflect.Name("message_oneof")).
			Fields().ByName(protoreflect.Name("message"))
)

func (x auxMessage) SetSubscriptions(v int32) {
	m := x.m.ProtoReflect()
	m.Set(subscriptionsFd, protoreflect.ValueOfInt32(v))
}

func (x auxMessage) SetMessage(v string) {
	m := x.m.ProtoReflect()
	m.Set(messageFd, protoreflect.ValueOfString(v))
}
