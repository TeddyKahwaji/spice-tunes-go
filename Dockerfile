FROM golang:1.23 AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./

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

RUN curl -L https://github.com/yt-dlp/yt-dlp/archive/08f40e890b2d8d07adc2ef922530e34b7381e9c3.tar.gz -o /tmp/yt-dlp.tar.gz \
    && tar -xzf /tmp/yt-dlp.tar.gz -C /tmp \
    && make -C /tmp/yt-dlp-08f40e890b2d8d07adc2ef922530e34b7381e9c3 \
    && mv /tmp/yt-dlp-08f40e890b2d8d07adc2ef922530e34b7381e9c3/yt-dlp /usr/local/bin/yt-dlp \
    && chmod a+x /usr/local/bin/yt-dlp \
    && rm -rf /tmp/yt-dlp*

COPY --from=builder /go/bin/app /go/bin/app


EXPOSE 8080

CMD ["/go/bin/app"]
