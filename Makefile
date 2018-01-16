TARGET:=haproxy_agent

all: ${TARGET}

${TARGET}: $(shell find . -name '*.go')
	GOOS=linux GOARCH=amd64 go build -o $@ 

test:
	go test ./... -v

clean:
	rm -f ${TARGET}

.PHONY: all
.PHONY: clean
.PHONY: test
