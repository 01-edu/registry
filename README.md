# 🐳 registry

Self-hosted Docker registry with automated build services.

---

#### ⚠️ Deprecations:

<details> 
<summary> Automated builds: </summary>

[`build.json`](https://github.com/01-edu/registry/blob/master/build.json)

<sub> The legacy build list from the URL above is being deprecated due to the new method of hosting images through the GitHub Container Registry </sub>

</details>

#### 📦 Packages:

Our GitHub Container Registry package list can be seen here:

- [01 Packages](https://github.com/orgs/01-edu/packages)

## Installation

### Registry

```bash
docker run --detach --publish 5000:5000 --restart=unless-stopped --name registry --volume registry:/var/lib/registry registry:2.7.1
```

### Cloning

Clone this repository in `/opt` and enter the directory.

```bash
cd /opt
git clone git@github.com:01-edu/registry.git
cd /opt/registry
```

### Web server

Install [Caddy](https://caddyserver.com/docs/download#debian-ubuntu-raspbian), add the [Caddyfile](Caddyfile) to `/etc/caddy/Caddyfile` and reload it:

```bash
systemctl reload caddy
```

### Automated build service

First time only (to allow the service to push to the Docker registry) :

```bash
docker login docker.01-edu.org
```

```bash
go build
./registry -port 8081 2>log.txt &
```

Check that the images are correctly built:

```bash
tail -f log.txt
```

After a moment, you should see some messages similar to this:

```log
2021/04/08 16:20:01 docker [pull alpine:3.13.2]
2021/04/08 16:20:03 docker [tag alpine:3.13.2 docker.01-edu.org/alpine:3.13.2]
2021/04/08 16:20:03 docker [push docker.01-edu.org/alpine:3.13.2]
```

To make it start with the system, edit cron jobs:

```bash
crontab -e
```

Add this line:

```bash
@reboot /usr/bin/env bash -l sleep 10; /opt/registry/restartRegistry.sh
```

Save & exit.

## Usage

To pull from this registry, you need to log in first (with the password defined in [Caddyfile](Caddyfile)):

```bash
docker login docker.01-edu.org
```

To check if the service is working correctly, check the [logs](https://webhook.docker.01-edu.org/log.txt).

#### Add an image

- To build from a Git repository: edit [build.json](build.json).
- To mirror an already existing image: edit [mirror.json](mirror.json).
- To make a `PUT` HTTP request to webhooks: edit [webhooks.json](webhooks.json).

If you edit those files directly on GitHub or push them, the service will pull the new changes and take them into account.

#### Trigger

Manually trigger a rebuild (because the webhook wasn't configured correctly); here is an example with github.com/01-edu/public:

```bash
curl https://webhook.docker.01-edu.org -d'{"ref":"refs/heads/master","repository":{"ssh_url":"git@github.com:01-edu/public.git"}}'
```

#### Maintenance

To remove dangling images in the registry:

```bash
docker exec registry bin/registry garbage-collect /etc/docker/registry/config.yml --delete-untagged=true
```

## Re-launching

To relaunch the registry service, you can run the `restartRegistry.sh` script.

```bash
cd /opt/registry
./restartRegistry.sh
```

## Reasons

### Why - Context

- Docker Hub is taking a very long time to build (up to half an hour)
- Now rate limits the pulls
- Is quite easy to re-implement

### What - Solution

Deploying a web server, a registry (as a Docker container), and an automated build service on a dedicated server guarantees unlimited pulls & fast builds.

### How - Decision

Following the [official guide](https://docs.docker.com/registry) but using [Caddy](https://caddyserver.com) to handle HTTPS (because it's easier), authentication (because it's easier) and proxy to the service.
