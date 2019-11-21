FROM registry.sensetime.com/docker.io/golang:1.12.7-stretch
ARG BUILD_DIR=/go/src/github.com/mackwong/clickhouse-operator/
COPY . ${BUILD_DIR}
WORKDIR ${BUILD_DIR}
RUN go build -o ${BUILD_DIR}/bin/clickhouse-operator ./cmd/manager/main.go

FROM  registry.sensetime.com/docker.io/ubuntu:16.04
ARG BUILD_DIR=/go/src/github.com/mackwong/clickhouse-operator/
WORKDIR /clickhouse-operator
COPY --from=0 ${BUILD_DIR}/bin/clickhouse-operator /usr/local/bin/clickhouse-operator
COPY config.yaml /clickhouse-operator/config.yaml
COPY xml /clickhouse-operator/xml
ENTRYPOINT ["clickhouse-operator"]
