# docker.01-edu.org

Our own Docker registry.

## Why - Context

- Docker Hub is taking a very long time to build (up to half an hour)
- Now rate limits the pulls
- Is quite easy to reimplement

## What - Solution

Deploying a web server, a registry (as a Docker container) and an automated build service on a dedicated server guarantees unlimited pulls & fast builds.

## How - Decision

Following the [official guide](https://docs.docker.com/registry) but using [Caddy](https://caddyserver.com) to handle HTTPS (because it's easier), authentication (because it's easier) and proxy to the service.

### Launch

```
docker run --detach --publish 5000:5000 --restart=unless-stopped --name registry registry:2.7.1
```

Install [Caddy](https://caddyserver.com/docs/download#debian-ubuntu-raspbian)

Add this configuration to Caddy config file (`/etc/caddy/Caddyfile`) :

```
{
    email dev@01-edu.org
}

https://docker.01-edu.org {
    header Strict-Transport-Security max-age=31536000;
    basicauth {
        root JDJhJDEwJFdPRXN4anpLME5tQ3d2NGRKR0VidS5DUWNSUGY3Z0ZITFEwUU82MFJlMjhXYXd6RE16UG1x
    }
    reverse_proxy localhost:5000
}
```

Reload Caddy :

```
systemctl reload caddy
```
