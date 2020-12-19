# pair

2 parts

* server for hosting webrtc session descriptions
* client for hosting/joining a session

based on work by https://github.com/maxmcd/webtty

[Demo video](https://drive.google.com/file/d/1cle0Xyy9H3ih6IsoGq8K6UYGbrYNBoN8/view?usp=sharing)

## How-To
* requires `pair` and `tmux` installed
* client host must be started in a tmux session
* client guest must not be started in a tmux session

```sh
# host
$ pair

Share this command with your guest:
  pair http://<some url>
```
```sh
# guest
$ pair http://<url from host>
```

Setup local testing server with [mkcert](https://mkcert.dev/):
```sh
mkcert -install
mkdir -p certs
cd certs
mkcert localhost.dev
mkcert localhost
mkcert <someotherdomain>
cd ..
# append '127.0.0.1 localhost.dev' to /etc/hosts file
pair-server -v -domain localhost.dev
```

Run server in production with [acme](https://pkg.go.dev/golang.org/x/crypto/acme/autocert):
```sh
# ensure chosen domain is registered and can access public server on ports 80 and 443
mkdir -p certs
pair-server -v -domain <chosen-domain>
```

## TODO
* refactor
* add tests
* copy command to hosts clipboard
