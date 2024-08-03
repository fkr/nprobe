GOBIN=/opt/homebrew/bin/go

default:
	@echo "Targets: clean, build, run-client, run-server"

clean:
	rm -rf nprobe

build:
	$(GOBIN) build

docker:
	docker buildx build --file Dockerfile.no_goreleaser --platform linux/amd64 --tag ghcr.io/fkr/nprobe:latest --push .

run-client: build
	sudo NPROBE_SECRET=secret ./nprobe -debug -head 127.0.0.1:8000 -notls -name localhost-probe -privileged

run-server: build
	./nprobe -debug -mode head -config config/config-no-db.json
