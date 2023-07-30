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

// Package main implements a simple gRPC client that interacts with the pubsub service whose definition can be found in pubsub/pubsub.proto.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/weiwenchen2022/grpc-pubsub/pubsub"
)

var serverAddr = flag.String("addr", "127.0.0.1:50051", "The server address in the format of host:port")

const version = "0.1.0"

func init() {
	log.SetFlags(0)

	const usage1 = `%s %s
Usage: %[1]s [options] [cmd [arg [arg ...]]]
`

	const usage2 = `
Examples:
	%s -h
	%[1]s sub hello
	%[1]s pub hello alice

When no command is given, %[1]s starts in interactive mode.
Type "help" in interactive mode for information on available commands.
`

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage1, os.Args[0], version)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), usage2, os.Args[0])
	}
}

func main() {
	flag.Parse()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewPubsubClient(conn)
	if flag.NArg() == 0 {
		startRepl(client)
		return
	}

	commandName := flag.Arg(0)
	command := getCommand(commandName)
	if err := command.callback(client, flag.Args()[1:]...); err != nil {
		log.Fatal(err)
	}
}
