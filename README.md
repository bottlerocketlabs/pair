# pair

2 parts

* server for hosting webrtc session descriptions
* client for hosting/joining a session

based on work by https://github.com/maxmcd/webtty

[Demo video](https://drive.google.com/file/d/1cle0Xyy9H3ih6IsoGq8K6UYGbrYNBoN8/view?usp=sharing)

## How-To
* requires [`pair`](https://github.com/stuart-warren/pair/releases) and `tmux` installed
* client host must be started in a tmux session
* client guest must not be started in a tmux session

Start by hosting a session:
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

Optionally setup a local testing server and use it:
```sh
$ pair-server -v &
$ pair -v -sdp http://localhost
```

## TODO
* refactor
* add more tests
* copy command to hosts clipboard
