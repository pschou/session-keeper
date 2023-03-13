# Session-Keeper

A session maintainer for TCP connections.

When firewall updates happen, often state-aware connections are terminated; this means all active connections will be terminated.  Ultimately this will disrupt active TCP connections (like SSH), is there a better solution?  Enter stage left Session-Keeper.  The primary intent of the Session-Keeper is to maintain the state of a TCP session where otherwise state would be lost.

There are two components to Session-Keeper, the keeper and the server.  The server must reside on the end box behind the firewall and preferably on the host you intend to connect to.  The keeper will reside on your local desktop, acting like an HTTP CONNECT proxy.  One then points the keeper to the server and the local TCP session (IE: putty) to the HTTP proxy.  The session-keeper will see the request, attempt to proxy the connection to the server, and then re-establish the connection anytime a TCP termination happens while resuming the previous session.

NOTE: This is not an encrypted protocol, so it is intended to wrap another encrypted protocol.  As the UUID of each connection is sent on each re-establishment of a connection, the chance of a man-in-the-middle happening is increased, so the internal protocol should be resilient enough to terminate a session or handle noise appropriately.

A typical command-line invocation may look like this:
```
desktop$ ./session-keeper -target server:2020
Listening on :2222
```

```
server$ ./session-server
Listening on :2020
```
