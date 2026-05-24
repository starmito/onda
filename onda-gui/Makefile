VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")

.PHONY: build deploy rebuild

build:
	docker build --build-arg ONDA_VERSION=$(VERSION) -t onda-gui .

deploy:
	docker stop onda-gui 2>/dev/null || true
	docker rm onda-gui 2>/dev/null || true
	docker run -d --name onda-gui --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /home/starmito/projects/onda/input:/input \
		-v /home/starmito/projects/onda/output:/output \
		onda-gui

rebuild: build deploy
