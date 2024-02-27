FROM golang:latest

WORKDIR /app
COPY ./app /app
RUN go build -o main .
CMD ["./main"]
