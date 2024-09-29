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
    apt-get install -y --no-install-recommends ca-certificates ffmpeg unzip python3 && \
    apt-get clean autoclean && \
    rm -rf /var/lib/apt/lists/*

RUN curl -L https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/download/2024.09.27.232842/yt-dlp -o /usr/local/bin/yt-dlp \
    && chmod a+x /usr/local/bin/yt-dlp

RUN mkdir -p ~/.config/yt-dlp/plugins/

# Install yt-dlp OAuth2 plugin
RUN curl -L https://github.com/coletdjnz/yt-dlp-youtube-oauth2/releases/download/v2024.9.14/yt-dlp-youtube-oauth2.zip -o /usr/local/bin/yt-dlp-oauth2.zip && \
    mkdir -p ~/.config/yt-dlp-plugins/something/ && \
    unzip /usr/local/bin/yt-dlp-oauth2.zip -d ~/.config/yt-dlp-plugins/something/ && \
    chmod -R a+x ~/.config/yt-dlp/plugins/ && \
    rm /usr/local/bin/yt-dlp-oauth2.zip

COPY --from=builder /go/bin/app /go/bin/app


EXPOSE 8080

CMD ["/go/bin/app"]
