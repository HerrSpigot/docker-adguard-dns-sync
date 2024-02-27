# Docker-AdGuard-DNS-Sync
Docker application to monitor the labels of running containers to synchronize the DNS records with AdGuard Home using the Docker API for Go and the AdGuard Home Rest-API.

> [!WARNING]
> This application is still under development.
> Using it may require additional steps when updating to newer versions until the final release.

## Usage
A Docker container must have a label `syncdns.rewrites` so that the application automatically manages the DNS entries in AdGuard-Home.
```
services:
  hello-world:
    container_name: hello-world
    image: hello-world:latest
    labels:
      - "syncdns.rewrites=Rewrite('hello-world.local', '127.0.0.1')"
```
Several DNS entries can also be assigned to a container.
The number of possible entries is unlimited.
It is mandatory to enter the IP address.
This also allows entries to be stored for the containers that should only be accessible via a reverse proxy (e.g. traefik).
```
services:
  hello-world:
    container_name: hello-world
    image: hello-world:latest
    labels:
      - "syncdns.rewrites=Rewrite('hello-world.local', '127.0.0.1') || Rewrite('hello-world.local2', '127.0.0.1') || Rewrite('hello-world3.local', '127.0.0.1')"
```

## Ready-to-use images / Docker Hub
Ready-to-use images are available on Docker Hub for the linux/amd64 and linux/arm64 architectures.

The following image tag can be used for the latest version of the respective architectures:

linux/amd64:
x86-latest / latest

linux/arm64:
arm-latest

All tags can be found on the following page:
https://hub.docker.com/repository/docker/herrspigot/docker-adguard-dns-sync/tags

## Deployment (ready-to-use images)

Before deploying this application or starting the container, a functioning and started instance of AdGuard-Home must already be available.

A directory on the host or a Docker volume is required for deployment with Docker Compose.

### Deployment with Docker Compose (recommended)
#### docker-compose.yml
```
services:
  adguard_sync:
    container_name: ${containerName}
    image: herrspigot/docker-adguard-dns-sync:latest
    environment:
      AdguardURL: ${AdguardURL}
      AdguardUser: ${AdguardUser}
      AdguardPassword: ${AdguardPassword}
    volumes:
      - "${dataDir}:/data"
      - "/var/run/docker.sock:/var/run/docker.sock"
    env_file:
      - .env
```
#### .env file
```
containerName=adguard_sync
AdguardURL=http://adguard.local
AdguardUser=syncuser
AdguardPassword=syncpassword

dataDir=/path/to/appdata
```
The values shown in the example must be entered to start the container, otherwise it will not start.
It is also recommended to set a restart policy (at least "unless-stopped").

## Deployment by starting the container with "docker-run"
```
docker run \
--name adguard_sync \
-e AdguardURL=http://adguard.local \
-e AdguardUser=syncuser \
-e AdguardPassword=syncpassword \
-v /path/to/appdata:/data \
-v /var/run/docker.sock:/var/run/docker.sock \
herrspigot/docker-adguard-dns-sync:latest
```

## Environment values
| Name                 | Description                                                              |
|----------------------|--------------------------------------------------------------------------|
| AdguardURL           | Base URL of the AdGuard home instance with specification of the HTTP or HTTPS prototoll |
| AdguardUser          | User name of a user with administrative rights for DNS rewrites in AdGuard-Home |
| AdguardPassword      | Password of the specified user |
| DNSOverwrite  #to-do | Allows you to take over the management of existing DNS rewrite entries if they are identical to a container |

## Build yourself
If the image is not available for a specific architecture or the code needs to be adapted for your own purposes, you can of course build the image yourself.
Once a Go-Mod file has been initiated in the `/app` directory, all the necessary files are available in this repository.