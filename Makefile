run:
	go build -o ./bin/osint *.go && ./bin/osint

r:
	./bin/osint

build:
	go build -o ./bin/osint *.go
