package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	pb "github.com/weiwenchen2022/grpc-pubsub/pubsub"
)

func startRepl(client pb.PubsubClient) {
	sc := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("Pubsub > ")

		if !sc.Scan() {
			break
		}
		words := cleanInput(sc.Text())
		if len(words) == 0 {
			continue
		}

		commandName := words[0]
		command := getCommand(commandName)
		if err := command.callback(client, words[1:]...); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
}

func cleanInput(text string) []string {
	return strings.Fields(strings.TrimSpace(text))
}

type cliCommand struct {
	name        string
	description string
	callback    func(client pb.PubsubClient, args ...string) error
}

var commands map[string]cliCommand

var unknownCommand = cliCommand{
	name:        "unknown",
	description: "unknown command",
	callback: func(pb.PubsubClient, ...string) error {
		return errors.New("unknown command")
	},
}

func getCommand(name string) cliCommand {
	if command, exists := commands[strings.ToLower(name)]; exists {
		return command
	}
	return unknownCommand
}

func commandHelp(pb.PubsubClient, ...string) error {
	const usage = `
Welcome to the Pubsub!
Usage:

`
	fmt.Print(usage)

	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		cmd := commands[n]
		fmt.Printf("%s: %s\n", cmd.name, cmd.description)
	}
	fmt.Println()

	return nil
}

func commandQuit(pb.PubsubClient, ...string) error {
	os.Exit(0)
	return nil
}

func init() {
	commands = map[string]cliCommand{
		"help": {
			name:        "help",
			description: "Displays a help message",
			callback:    commandHelp,
		},

		"exit": {
			name:        "exit",
			description: "Exit the Pubsub",
			callback:    commandQuit,
		},

		"sub": {
			name:        "subscribe channel...",
			description: "Subscribes the client to the specified channels.",
			callback:    commandSubscribe,
		},

		"pub": {
			name:        "publish channel message",
			description: "Posts a message to the given channel.",
			callback:    commandPublish,
		},

		"channels": {
			name: "channels [pattern]",
			description: `Lists the currently active channels.
An active channel is a Pub/Sub channel with one or more subscribers (excluding clients
subscribed to patterns).

If no pattern is specified, all the channels are listed, otherwise if pattern is specified
only channels matching the specified glob-style pattern are listed.
`,
			callback: commandChannels,
		},

		"numsub": {
			name: "numsub [channel...]",
			description: ` Returns the number of subscribers (exclusive of clients subscribed to patterns) for the
specified channels.

Note that it is valid to call this command without channels. In this case it will just return
an empty list.
`,
			callback: commandNumSub,
		},

		"psub": {
			name: "psub pattern...",
			description: `Subscribes the client to the given patterns.
Supported glob-style patterns:

	- h?llo subscribes to hello, hallo and hxllo
	- h*llo subscribes to hllo and heeeello
	- h[ae]llo subscribes to hello and hallo, but not hillo

Use \ to escape special characters if you want to match them verbatim.
`,
			callback: commandPSubscribe,
		},

		"numpat": {
			name: "numpat",
			description: `Returns the number of unique patterns that are subscribed to by clients (that are
performed using the psub command).

Note that this isn't the count of clients subscribed to patterns, but the total number of
unique patterns all the clients are subscribed to.
`,
			callback: commandNumPat,
		},
	}
}
