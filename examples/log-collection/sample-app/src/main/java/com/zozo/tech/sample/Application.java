package com.zozo.tech.sample;

import static net.logstash.logback.argument.StructuredArguments.entries;
import static net.logstash.logback.argument.StructuredArguments.kv;
import static net.logstash.logback.marker.Markers.append;

import java.util.HashMap;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class Application {
  private static final Logger logger = LoggerFactory.getLogger(Application.class);
  private static final Logger eventLogger = LoggerFactory.getLogger("event-logger");

  public static void main(String[] args) throws InterruptedException {
    int i = 0;
    while (true) {
      i++;
      Thread.sleep(1000L);

      Map<String, Object> myMap = new HashMap<String, Object>();
      myMap.put("count", i);
      myMap.put("myKey", "myValue");
      eventLogger.info(append("event_version", 1), "test-event", entries(myMap));
      eventLogger.info(
          append("event_version", 1),
          "test-event",
          kv("count", i),
          kv("myKey", "myValue"),
          kv("optionalKey", "optionalValue"));
      logger.info(String.format("main: %d", i));
    }
  }
}
