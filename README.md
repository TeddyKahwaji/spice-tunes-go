# Discord Music Bot

This Discord music bot, written in Go, allows users to stream music from a variety of sources, including YouTube and Spotify. It features a modular design, including queue management, pagination for the music queue, and track swapping functionality.

## Key Features

- **Play Music**: Stream music from multiple platforms such as YouTube and Spotify.
- **Queue System**: Add, remove, and reorder tracks in the music queue.
- **Pagination**: View and interact with the queue using paginated embeds.
- **Track Swapping**: Swap two tracks in the queue with a simple command.
- **Error Handling**: Robust error handling to ensure smooth user interaction.
- **Logger Integration**: Uses Zap logger for detailed logging throughout the bot.

## Installation

### Prerequisites

- **Go**: Make sure Go is installed on your machine (version 1.23 or later).
- **Discord Bot Token**: You need to create a bot on Discord and get a token. You can follow the [Discord Developer Portal](https://discord.com/developers/docs/intro) to get your bot set up.
- **yt-dlp**: This bot uses `yt-dlp` for music streaming from YouTube. You can install it via binary.
- **FFmpeg**: Ensure that `ffmpeg` is installed for audio processing.

### Setup

1. Clone this repository:
    ```bash
    git clone https://github.com/your-username/discord-music-bot.git
    cd discord-music-bot
    ```

2. Set up environment variables:
    ```bash
    export DISCORD_TOKEN=your_discord_bot_token
    export SPOTIFY_CLIENT_ID=your_spotify_client_id
    export SPOTIFY_CLIENT_SECRET=your_spotify_client_secret
    ```

3. Build and run the bot:
    ```bash
    go build -o music-bot ./cmd
    ./music-bot
    ```

### Docker Setup

You can also use Docker to run the bot:

1. Build the Docker image:
    ```bash
    docker build -t music-bot .
    ```

2. Run the Docker container:
    ```bash
    docker run -e DISCORD_TOKEN=your_discord_bot_token -e SPOTIFY_CLIENT_ID=your_spotify_client_id -e SPOTIFY_CLIENT_SECRET=your_spotify_client_secret music-bot
    ```

## Usage

### Commands

- **/play [URL or Search]**: Plays a track from the provided URL or search query.
- **/queue**: Displays the current music queue.
- **/skip**: Skips the current track.
- **/swap [firstPosition] [secondPosition]**: Swaps two tracks in the queue.
- **/pause**: Pauses the currently playing track.
- **/resume**: Resumes a paused track.

### Queue Pagination

The bot uses paginated embeds to display the music queue, making it easier to browse through large queues. You can navigate through the queue using buttons provided below each embed.

## Configuration

You can customize some aspects of the bot by modifying the configuration files. Ensure that your credentials for various services are correctly set up in the `config/credentials.json` file.

## Logging

The bot uses Zap logger for logging purposes. You can configure logging settings in the `zap_config.json` file to adjust verbosity or log formatting.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
