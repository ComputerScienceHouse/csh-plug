#!/bin/bash

# Use this to set up a test container on your local machine

ENV_SECRETS='.env' # Add secrets file
podman run --rm -it --env-file=$ENV_SECRETS -p 8080:8080 --name=plug-dev csh-plug:latest

