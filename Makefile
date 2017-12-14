TARGET:=haproxy_agent

all: ${TARGET}

${TARGET}: $(shell find . -name '*.go')
	GOOS=linux GOARCH=amd64 go build -o $@ 

clean:
	rm -f ${TARGET}

.PHONY: all
.PHONY: clean
