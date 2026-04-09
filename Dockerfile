FROM python:3.14.4-alpine3.23

COPY requirements.txt requirements.txt
RUN pip3 install --no-cache-dir -r requirements.txt

COPY main.py /app/main.py
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser -s /bin/sh appuser && \
    chown -R appuser:appuser /app
WORKDIR /app
USER appuser
CMD ["python3", "main.py"]
