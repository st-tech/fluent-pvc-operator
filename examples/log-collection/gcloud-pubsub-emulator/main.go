package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/pubsub"
)

func main() {
	var (
		help = flag.Bool("help", false, "Display usage information")
		pj   = flag.String("project", "my-pubsub-project", "GCP Project ID")
		t    = flag.String("topic", "my-topic", "Pub/Sub Topic Name")
		s    = flag.String("subscription", "my-subscription", "Pub/Sub Subscription Name")
		echo = flag.Bool("echo", false, "Subscribe messages with echo.")
	)
	flag.Parse()
	flag.Usage = func() {
		flag.PrintDefaults()
	}
	if *help {
		flag.Usage()
		return
	}

	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, *pj)
	if err != nil {
		fmt.Printf("Unable to create client for project %s: %s\n", *pj, err)
		os.Exit(1)
	}
	defer client.Close()

	if _, err := client.CreateTopic(ctx, *t); err != nil {
		if !strings.Contains(err.Error(), "code = AlreadyExists") {
			fmt.Printf("Unable to create the topic %s: %s\n", *t, err)
			os.Exit(1)
		}
	}
	if _, err = client.CreateSubscription(ctx, *s, pubsub.SubscriptionConfig{Topic: client.Topic(*t)}); err != nil {
		if !strings.Contains(err.Error(), "code = AlreadyExists") {
			fmt.Printf("Unable to create the subscription %s: %s\n", *s, err)
			os.Exit(1)
		}
	}

	if *echo {
		sub := client.Subscription(*s)
		for {
			sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
				fmt.Printf("Got message: %s\n", msg.Data)
				msg.Ack()
			})
		}
	}

}
