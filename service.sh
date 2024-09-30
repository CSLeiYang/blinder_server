#!/bin/bash

SERVICE_NAME="yanglei_blinder"
LOG_FILE="blinder.log"

start() {
    echo "Starting $SERVICE_NAME..."
    nohup ./$SERVICE_NAME > $LOG_FILE 2>&1 &
    echo "$SERVICE_NAME started with PID $!"
}

stop() {
    echo "Stopping $SERVICE_NAME..."
    PID=$(pgrep -f $SERVICE_NAME)
    if [ -n "$PID" ]; then
        kill $PID
        echo "$SERVICE_NAME stopped."
    else
        echo "$SERVICE_NAME is not running."
    fi
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    *)
        echo "Usage: $0 {start|stop}"
        exit 1
        ;;
esac
