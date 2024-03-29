#EXAMPLE_NAME = sc
#TARGET_LINUX = GOARCH=amd64 GOOS=linux

.PHONY: all
all: test_build test code_qa

.PHONY: test
test:
	cd api && go test -covermode=atomic
	cd auth && go test -covermode=atomic
	cd cli && go test -covermode=atomic
	cd config && go test -covermode=atomic
	go test -covermode=atomic

.PHONY: code_qa
code_qa:
	#go test `go list ./... | grep -v 'hack\|deprecated'` -coverprofile=coverage.txt -covermode=atomic
	golangci-lint run > lint.txt

.PHONY: test_build
test_build:
	go mod verify && go mod tidy
	cd cmd/router && go build main.go && rm main
	cd examples/service && go build main.go && rm main
	cd examples/cli && go build cli.go && rm cli
	cd examples/appengine && go build main.go && rm main


#.PHONY: examples
#examples: example_cli example_service

#.PHONY: example_appengine
#example_appengine:
#	cd examples/appengine && gcloud app deploy . --quiet

#.PHONY: example_cli
#example_cli:
#	cd examples/cli && go build -o ${EXAMPLE_NAME} cli.go && mv ${EXAMPLE_NAME} ../../bin/${EXAMPLE_NAME}
#	chmod +x bin/${EXAMPLE_NAME}

#.PHONY: example_service
#example_service:
#	cd examples/service && go build -o svc main.go && mv svc ../../bin/${EXAMPLE_NAME}svc
#	chmod +x bin/${EXAMPLE_NAME}svc

#.PHONY example_api_container
#example_api_container:
#	cd examples/simple_api && ${TARGET_LINUX} go build -o svc main.go && podman build -t "" .
#	rm examples/simple_api/svc
