# Description
The grpc-pubsub server and client use grpc go libraries and pubsub library to
implement pubsub service.

Please refer to [pubsub](https://pkg.go.dev/github.com/weiwenchen2022/pubsub) for more information.

See the definition of the pubsub service in `pubsub/pubsub.proto`.

# Run the sample code
To compile and run the server, assuming you are in the root of the `grpc-pubsub`
folder, i.e., `.../grpc-pubsub/`, simply:

```sh
$ go run ./pubsub-server
```

Likewise, to run the client:

```sh
$ go run ./pubsub-cli
```
