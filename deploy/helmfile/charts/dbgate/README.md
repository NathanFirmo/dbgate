# dbgate Helm Chart

`dbgate` is a lightweight HTTP database gateway for test workflows. This chart deploys the `dbgate` server into Kubernetes and renders its database configuration into a mounted `dbgate.yaml` ConfigMap.

## Values

- `image.repository`: container image repository
- `image.tag`: container image tag
- `image.pullPolicy`: image pull policy
- `service.type`: Kubernetes service type, default `ClusterIP`
- `service.port`: service and container port, default `9999`
- `service.nodePort`: optional fixed NodePort when `service.type` is `NodePort`
- `config.databases`: list of MySQL and MongoDB database definitions rendered into `dbgate.yaml`

## Install

```bash
helm repo add dbgate https://nathanfirmo.github.io/dbgate
helm install dbgate dbgate/dbgate -n test-tools --create-namespace
```

## Example values

```yaml
image:
  repository: docker.io/nathanfirmo/dbgate
  tag: latest

service:
  type: NodePort
  port: 9999
  nodePort: 30007

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
