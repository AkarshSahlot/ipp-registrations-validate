all:
	go build

clean:
	rm -f ipp-registrations-validate

test:
	go test ./...
