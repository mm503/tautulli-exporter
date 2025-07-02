FROM alpine:3.22.0

RUN apk upgrade --no-cache && \
  apk add --no-cache \
  python3 \
  py3-pip \
  py3-requests \
  py3-prometheus-client

COPY main.py /app/main.py
WORKDIR /app
USER daemon
CMD ["python3", "main.py"]
