all: frakti

frakti: frakti.go $(wildcard pkg/**/**.go)
	godep go build frakti.go

install:
	cp -f frakti /usr/local/bin

clean:
	rm -f frakti
