VERSION = 0.1.$(shell date +%Y%m%d.%H%M)
FLAGS := "-s -w -X main.version=${VERSION}"
#WFLAGS := "-s -w -X main.version=${VERSION} -H=windowsgui"

build:
	GOOS=linux GOARCH=amd64 GO111MODULE=on CGO_ENABLED=0 go build -ldflags=${FLAGS} -o session-keeper session-keeper.go session-keeper-linux.go lib-*.go
	GOOS=linux GOARCH=amd64 GO111MODULE=on CGO_ENABLED=0 go build -ldflags=${FLAGS} -o session-server session-server.go lib-*.go
	GOOS=windows GOARCH=amd64 GO111MODULE=on CGO_ENABLED=0 go build -ldflags=${FLAGS} \
		-o session-keeper.exe session-keeper.go session-keeper-win.go lib-*.go

