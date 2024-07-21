FROM golang:1.21

WORKDIR /opt/metrics

COPY go.mod /opt/metrics/
COPY go.sum /opt/metrics/
RUN go mod download

COPY cmd/ /opt/metrics/cmd/
COPY pkg/ /opt/metrics/pkg/
RUN go build -o /opt/metrics/bin/metrics cmd/metrics/main.go

COPY bundlers.txt .

CMD [ "/opt/metrics/bin/metrics" ]