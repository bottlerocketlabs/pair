# pair

A tool for sharing your terminal tmux session with others across the interwebs

Useful for pair programming with terminal based editors (vim/emacs)

2 parts

* server for sharing webrtc session descriptions (instance hosted on heroku)
* client for hosting/joining a session

based on work by https://github.com/maxmcd/webtty and https://github.com/nwtgck/go-piping-server

[![Demo Video Screenshot](https://user-images.githubusercontent.com/3208285/103408914-60521a80-4b5c-11eb-82e7-d7564eca424b.png)](https://user-images.githubusercontent.com/3208285/103408760-d3a75c80-4b5b-11eb-8271-b0efcd5330ba.mp4)

## Install

### [Homebrew / Linuxbrew](https://brew.sh/)

Should probably work within a [WSL2 terminal on Windows 10](https://docs.microsoft.com/en-us/windows/wsl/install-win10) also
```
brew install tmux bottlerocketlabs/apps/pair
```
### Manually

Download `pair` binaries from [releases](https://github.com/bottlerocketlabs/pair/releases) and put on path, you need to also install `tmux` for your platform (apt-get etc)

## How-To

* client host must be started in a tmux session
* client guest must not be started in a tmux session

Start by hosting a session within tmux:
```sh
# host
$ pair

Share this command with your guest:
  pair http://<some url>
```
Invite a guest by quickly supplying the command output above
```sh
# guest
$ pair http://<url from host>
```

## Testing/Development

Optionally setup a simple (insecure) local testing server and use it:
```sh
$ pair-server-simple -v &
$ pair -v -sdp http://localhost
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
# append '127.0.0.1 localhost.dev <someotherdomain>' to /etc/hosts file
pair-server -v -domain localhost.dev
```

Run server in production with [acme](https://pkg.go.dev/golang.org/x/crypto/acme/autocert):
```sh
# ensure chosen domain is registered and can access public server on ports 80 and 443
mkdir -p certs
pair-server -v -domain <chosen-domain>
```

## TODO
* add more tests
* run tmux and host pair within a development docker container to restrict access
