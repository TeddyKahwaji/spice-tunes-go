FROM golang:1.23 AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./

COPY cookies.txt /app/cookies.txt

RUN go mod download

COPY . .

RUN go build -o /go/bin/app ./cmd

FROM golang:1.23

WORKDIR /usr/src/app

RUN useradd -m docker && echo "docker:docker" | chpasswd && adduser docker sudo


RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg unzip zip pandoc && \
    apt-get clean autoclean && \
    rm -rf /var/lib/apt/lists/*


RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp \
    && chmod a+x /usr/local/bin/yt-dlp

    
COPY --from=builder /go/bin/app /go/bin/app

# Copy cookies.txt to /app directory in the runtime image
COPY cookies.txt /app/cookies.txt

EXPOSE 8080

CMD ["/go/bin/app"]
