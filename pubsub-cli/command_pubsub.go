package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	pb "github.com/weiwenchen2022/grpc-pubsub/pubsub"
	"google.golang.org/protobuf/proto"
)

func commandSubscribe(client pb.PubsubClient, channels ...string) error {
	if len(channels) == 0 {
		return errors.New("no channels provided")
	}

	stream, err := client.Subscribe(context.Background())
	if err != nil {
		return fmt.Errorf("client.Subscribe failed: %w", err)
	}

	errorc := make(chan error, 1)
	go func() {
		defer close(errorc)
		for {
			in, err := stream.Recv()
			switch err {
			default:
				errorc <- fmt.Errorf("client.Subscribe failed: %w", err)
				fallthrough
			case io.EOF:
				return
			case nil:
			}

			switch typ := in.GetType(); typ {
			default:
				log.Printf("unknown message type - %s", typ)
			case "subscribe":
				log.Printf("subscribe: channel: %q, subscriptions: %d", in.GetChannel(), in.GetSubscriptions())
			case "unsubscribe":
				log.Printf("unsubscribe: channel: %q, subscriptions: %d", in.GetChannel(), in.GetSubscriptions())
			case "message":
				log.Printf("message: channel: %q, message: %q", in.GetChannel(), in.GetMessage())
			}
		}
	}()

	request := &pb.SubscribeRequest{Type: "subscribe", Channels: channels}
	if err := stream.Send(request); err != nil {
		return fmt.Errorf("client.Subscribe: stream.Send(%v) failed: %w", request, err)
	}

	signalc := make(chan os.Signal, 1)
	signal.Notify(signalc, os.Interrupt)
	defer signal.Stop(signalc)

	log.Printf("Reading messages from %q... (press Ctrl-C to quit)", channels)
	<-signalc

	request = &pb.SubscribeRequest{Type: "unsubscribe", Channels: channels}
	if err := stream.Send(request); err != nil {
		return fmt.Errorf("client.Subscribe: stream.Send(%v) failed: %w", request, err)
	}
	err = stream.CloseSend()

	if e := <-errorc; e != nil && err == nil {
		err = e
	}

	if err != nil {
		return err
	}
	return nil
}

func commandPublish(client pb.PubsubClient, args ...string) error {
	if len(args) != 2 {
		return errors.New("no enough arguments")
	}

	channel, message := args[0], args[1]
	request := &pb.PublishRequest{Channel: channel, Message: message}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	received, err := client.Publish(ctx, request)
	if err != nil {
		return fmt.Errorf("client.Publish failed: %w", err)
	}
	log.Println(received.GetReceived())
	return nil
}

func commandChannels(client pb.PubsubClient, args ...string) error {
	request := &pb.ChannelsRequest{}
	if len(args) == 1 {
		request.Pattern = proto.String(args[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	channels, err := client.Channels(ctx, request)
	if err != nil {
		return fmt.Errorf("client.Channels failed: %w", err)
	}
	log.Printf("%q\n", channels.GetChannels())
	return nil
}

func commandNumSub(client pb.PubsubClient, channels ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.NumSub(ctx, &pb.NumSubRequest{Channels: channels})
	if err != nil {
		log.Fatalf("client.NumSub failed: %v", err)
	}
	log.Println(result.GetResults())
	return nil
}

func commandPSubscribe(client pb.PubsubClient, patterns ...string) error {
	if len(patterns) == 0 {
		return errors.New("no patterns provided")
	}

	stream, err := client.PSubscribe(context.Background())
	if err != nil {
		return fmt.Errorf("client.PSubscribe failed: %w", err)
	}

	errorc := make(chan error, 1)
	go func() {
		defer close(errorc)
		for {
			in, err := stream.Recv()
			switch err {
			default:
				errorc <- fmt.Errorf("client.PSubscribe failed: %w", err)
				fallthrough
			case io.EOF:
				// read done.
				return
			case nil:
			}

			switch typ := in.GetType(); typ {
			default:
				log.Printf("unknown type - %s", typ)
			case "psubscribe":
				log.Printf("psubscribe, pattern: %q, subscriptions: %d", in.GetPattern(), in.GetSubscriptions())
			case "punsubscribe":
				log.Printf("punsubscribe, pattern: %q, subscriptions: %d", in.GetPattern(), in.GetSubscriptions())
			case "pmessage":
				log.Printf("pmessage, pattern: %q, channel: %q, message: %q",
					in.GetPattern(), in.GetChannel(), in.GetMessage())
			}
		}
	}()

	request := &pb.SubscribeRequest{Type: "psubscribe", Channels: patterns}
	if err := stream.Send(request); err != nil {
		return fmt.Errorf("client.PSubscribe: stream.Send(%v) failed: %v", request, err)
	}

	signalc := make(chan os.Signal, 1)
	signal.Notify(signalc, os.Interrupt)
	defer signal.Stop(signalc)

	log.Printf("Reading messages from %q... (press Ctrl-C to quit)", patterns)
	<-signalc

	request = &pb.SubscribeRequest{Type: "punsubscribe", Channels: patterns}
	if err := stream.Send(request); err != nil {
		return fmt.Errorf("client.PSubscribe: stream.Send(%v) failed: %w", request, err)
	}
	err = stream.CloseSend()

	if e := <-errorc; e != nil && err == nil {
		err = e
	}

	if err != nil {
		return err
	}
	return nil
}

func commandNumPat(client pb.PubsubClient, _ ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, err := client.NumPat(ctx, &pb.Null{})
	if err != nil {
		return fmt.Errorf("client.NumPat failed: %w", err)
	}
	log.Println(reply.GetInt64())
	return nil
}
