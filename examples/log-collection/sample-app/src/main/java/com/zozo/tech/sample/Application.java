package com.zozo.tech.sample;

import static net.logstash.logback.argument.StructuredArguments.entries;
import static net.logstash.logback.marker.Markers.append;

import java.util.HashMap;
import java.util.Map;
import java.util.Optional;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class Application {
  private static final Logger logger = LoggerFactory.getLogger(Application.class);
  private static final Logger eventLogger = LoggerFactory.getLogger("event-logger");

  public static void main(String[] args) throws InterruptedException {
    int benchmarkLoggingMaxLogCount =
        Integer.parseInt(
            Optional.ofNullable(System.getenv("BENCHMARK_LOGGING_MAX_LOG_COUNT"))
                .orElse(String.valueOf(Integer.MAX_VALUE)));
    int benchmarkLoggingIntervalMillis =
        Integer.parseInt(
            Optional.ofNullable(System.getenv("BENCHMARK_LOGGING_INTERVAL_MILLIS")).orElse("1000"));
    int logCount = 0;
    while (logCount < benchmarkLoggingMaxLogCount) {
      logCount++;

      int payloadFieldCount = 0;
      Map<String, Object> payload = new HashMap<String, Object>();
      while (true) {
        payloadFieldCount++;
        Optional<String> k =
            Optional.ofNullable(
                System.getenv(String.format("BENCHMARK_LOGGING_PAYLOAD_KEY%d", payloadFieldCount)));
        if (k.isEmpty()) break;
        String v =
            System.getenv(String.format("BENCHMARK_LOGGING_PAYLOAD_VALUE%d", payloadFieldCount));

        payload.put(k.get(), v);
      }

      String eventName =
          Optional.ofNullable(System.getenv("BENCHMARK_LOGGING_EVENT_NAME")).orElse("test-event");
      eventLogger.info(append("event_version", 1), eventName, entries(payload));
      Thread.sleep(benchmarkLoggingIntervalMillis);
    }
  }
}
