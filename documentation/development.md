# Development notes

## Docker buildx

1) setup builder

```
$ sudo docker buildx create --name mybuilder --use --bootstrap
```

2) run builder

```
$ docker buildx build --build-arg GIT_COMMIT=`git rev-list -1 HEAD` --build-arg BUILD_TIME="$(date +%Y-%m-%dT%H%M%S)" --push \
--platform linux/amd64,linux/arm64 \
--tag g.hazardous.org/nprobe/nprobe:latest-`git rev-list --abbrev-commit -1 HEAD` .
```

