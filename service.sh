#!/bin/bash

SERVICE_NAME="yanglei_blinder"
LOG_FILE="blinder.log"
RESTART_DELAY=5  # 重启延迟（秒）

start() {
    echo "Starting $SERVICE_NAME..."
    while true; do
        nohup ./$SERVICE_NAME >> $LOG_FILE 2>&1 &
        PID=$!
        echo "$SERVICE_NAME started with PID $PID"
        wait $PID
        echo "$SERVICE_NAME exited. Restarting..." >> $LOG_FILE
        sleep $RESTART_DELAY  # 添加重启延迟
    done
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

restart() {
    stop
    start
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    *)
        echo "Usage: $0 {start|stop|restart}"
        exit 1
        ;;
esac
