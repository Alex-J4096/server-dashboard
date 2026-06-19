FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /panel ./cmd/panel

FROM ubuntu:24.04
RUN useradd --system --uid 10001 --create-home panel
COPY --from=build /panel /usr/local/bin/panel
RUN mkdir -p /data /minecraft && chown -R panel:panel /data /minecraft
USER panel
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/panel"]
