# Session-Keeper

A session maintainer for TCP connections.  When firewall updates happen, often
state aware connections will be terminated in the idea that any active
connections will be re-established cleanly.  However, this can cause disruption
on active TCP connections (like SSH), enter stage left Session-Keeper.  The
primary intent of the Session-Keeper is to maintain state where otherwise state
would be lost.

There are two components to Session-Keeper, the keeper and the server.  The
server needs to reside on the end box which is behind the firewall and
preferribly on the host which you intend to connect to.  The keeper will reside
on your local desktop and acts just like a HTTP CONNECT proxy.  One then points
the keeper to the server and the local TCP session (IE: putty) to the HTTP
proxy.  The session-keeper will see the request, and attempt to proxy the
connection to the server and then will re-establish the connection anytime a
TCP termination happens while resuming the previous session.

NOTE: This is not an encrypted protocol, so it is intended to wrap another
protocol which is encrypted.  As the UUID of each connection is sent on each
re-establishment of a connection, the possibilities of man in the middle
happening is increased, so, the internal protocol should be resilent enough to
terminate a session or handle noise appropriately.

```
desktop$ ./session-keeper -target server:2020
Listening on :2222
```

```
server$ $ ./session-server
Listening on :2020
```
