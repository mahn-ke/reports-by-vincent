FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py .
RUN adduser --disabled-password --gecos "" app
USER app
EXPOSE 8080
CMD ["python", "main.py"]
