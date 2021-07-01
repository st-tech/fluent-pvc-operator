FROM gradle:6-jdk11 as builder
WORKDIR /app
COPY ./build.gradle ./
COPY ./gradle.lockfile ./
COPY ./settings.gradle ./
COPY ./src ./src/
RUN gradle build

FROM openjdk:11
COPY --from=builder /app/build/libs/sample-app-0.0.1.jar /
ENV TINI_VERSION v0.19.0
ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini /tini
RUN chmod +x /tini
ENTRYPOINT ["/tini", "--"]
CMD ["java", "-jar", "/sample-app-0.0.1.jar"]
