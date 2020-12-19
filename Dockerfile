FROM golang:1.15.6-alpine as builder
WORKDIR /go/src/github.com/stuart-warren/pair/
COPY . .
RUN go build ./...

FROM ubuntu:20.04
RUN apt update && \
    apt install -y \
    bash \
    ca-certificates \
    sudo \
    tmux \
    vim \
    zsh && \
    adduser --home /home/pair --gecos "" --disabled-password pair
USER pair
WORKDIR /home/pair
COPY --from=builder /go/src/github.com/stuart-warren/pair/pair /bin
COPY --from=builder /go/src/github.com/stuart-warren/pair/pair-server /bin
# ENV DOTFILES_REPO= # FIXME
ADD entrypoint /bin/entrypoint
ENTRYPOINT [ "/bin/entrypoint" ]