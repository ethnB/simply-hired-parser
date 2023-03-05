SRC := main.go

build_linux_amd64: $(SRC)
	GOOS=linux GOARCH=amd64 go build -o bin/simply-hired-parser $(SRC)

build_windows_amd64: $(SRC)
	GOOS=windows GOARCH=amd64 go build -o bin/simply-hired-parser.exe $(SRC)

build: build_linux_amd64 build_windows_amd64

clean:
	rm -rf bin/

.PHONY: clean