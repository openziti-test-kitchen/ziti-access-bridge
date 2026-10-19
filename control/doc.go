// Package control is the local control channel between a running ZAB and the zab agent
// command.
//
// A running ZAB listens on a unix domain socket, the socketFile setting in its
// configuration, and speaks HTTP over it. Serve runs that listener against a Source, which
// is whatever the running program exposes: its Status and a way to Stop it. Client connects
// to the same socket from another process and offers the same operations as methods. Both
// sides share the Status shape, so what the agent prints is what the running ZAB reported.
//
// Unix domain sockets work on Linux and on Windows 10 version 1803 and later, where Go's
// net package binds them as AF_UNIX. The socket path has to stay under the platform limit of
// about one hundred bytes, which is why the default sits beside the configuration file
// rather than under a deep temporary directory. Serve removes a stale socket file before
// listening and again when it stops, and on Linux limits the socket to its owner.
package control
