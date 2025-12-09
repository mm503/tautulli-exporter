FROM python:3.14.2-alpine3.22

COPY requirements.txt requirements.txt
RUN pip3 install -r requirements.txt

COPY main.py /app/main.py
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser -s /bin/sh appuser && \
    chown -R appuser:appuser /app
WORKDIR /app
USER appuser
CMD ["python3", "main.py"]
