FROM --platform=$TARGETPLATFORM golang:1.21 as builder
ARG TARGETPLATFORM
ARG BUILDPLATFORM

WORKDIR /work
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download && \
    go mod verify && \
    echo 'package main\nimport (\n_ "tailscale.com/tsnet"\n_ "tailscale.com/client/tailscale"\n)\nfunc main(){}' > main.go && \
    go build -ldflags="-s -w" -v ./ && \
    rm main.go

COPY . .

RUN go build -ldflags="-s -w" -v

FROM --platform=$TARGETPLATFORM scratch
COPY --from=builder /work/tsnsrv /usr/bin/tsnsrv

CMD ["/usr/bin/tsnsrv"]
