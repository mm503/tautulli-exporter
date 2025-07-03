FROM python:3.13.5-alpine3.22

COPY requirements.txt requirements.txt
RUN pip3 install -r requirements.txt

COPY main.py /app/main.py
WORKDIR /app
USER daemon
CMD ["python3", "main.py"]
