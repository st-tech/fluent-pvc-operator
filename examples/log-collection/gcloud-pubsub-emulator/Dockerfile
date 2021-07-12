FROM golang:alpine as builder
WORKDIR /work
COPY ./main.go ./
COPY ./go.mod ./
COPY ./go.sum ./
RUN go build -o pubsub-create .

FROM gcr.io/google.com/cloudsdktool/cloud-sdk:alpine
COPY --from=builder /work/pubsub-create /usr/bin/
COPY ./subscription.sh /subscription.sh
RUN apk add --no-cache tini openjdk8-jre \
	&& gcloud components install beta pubsub-emulator
ENV PUBSUB_PROJECT_ID my-pubsub-project
ENV PUBSUB_EMULATOR_HOST localhost:8085
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["gcloud", "beta", "emulators", "pubsub", "start", "--host-port", "0.0.0.0:8085"]
