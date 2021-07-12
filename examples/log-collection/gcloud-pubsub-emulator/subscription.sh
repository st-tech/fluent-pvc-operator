#!/bin/sh

pubsub-create \
	--project ${PUBSUB_PROJECT_ID} \
	--topic ${PUBSUB_EMULATOR_TOPIC} \
	--subscription ${PUBSUB_EMULATOR_SUBSCRIPTION} \
	--echo
