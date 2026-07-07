# dbgate Helm Chart

`dbgate` is a lightweight HTTP database gateway for test workflows. This chart deploys the `dbgate` server into Kubernetes and renders its database configuration into a mounted `dbgate.yaml` ConfigMap.

## Values

- `image.repository`: container image repository
- `image.tag`: container image tag
- `image.pullPolicy`: image pull policy
- `service.port`: service and container port, default `9999`
- `config.databases`: list of MySQL and MongoDB database definitions rendered into `dbgate.yaml`

## Install

```bash
helm repo add dbgate https://nathanfirmo.github.io/dbgate
helm install dbgate dbgate/dbgate -n test-tools --create-namespace
```

## Example values

```yaml
image:
  repository: docker.io/user/dbgate
  tag: latest

config:
  databases:
    - name: platform
      type: mysql
      dsn: root:mysqlroot@tcp(mysql.test.svc:3306)/platform
    - name: notifications
      type: mongo
      uri: mongodb://mongo.test.svc:27017
      database: notifications
```
