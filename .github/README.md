###### Production Setup (Systemd)
For a more robust setup, use `systemd` to manage the bot as a service. This ensures the bot restarts automatically if it crashes or the server reboots.

1.  **Install prerequisites:**
    - **On Debian/Ubuntu:**
      ```sh
      sudo apt update
      sudo apt install -y build-essential ffmpeg curl wget unzip git
      ```

    - **Install Deno** (required for YouTube download challenges):
      ```sh
      curl -fsSL https://deno.land/install.sh | sh
      echo 'export DENO_INSTALL="$HOME/.deno"' >> ~/.bashrc
      echo 'export PATH="$DENO_INSTALL/bin:$PATH"' >> ~/.bashrc
      source ~/.bashrc
      ```
      
    - **Install yt-dlp:**
      ```sh
      sudo wget https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -O /usr/local/bin/yt-dlp
      sudo chmod a+rx /usr/local/bin/yt-dlp
      ```

    - **Install Go** (1.26 or higher required):
      ```sh
      wget https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
      sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
      echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
      source ~/.bashrc
      ```


2.  **Clone the repository and create the `.env` file** as described in the [Configuration](#-configuration) section.

3.  **Generate necessary files:**
    ```sh
    go run setup_ntgcalls.go
    ```

    ```bash
    go run github.com/AshokShau/gotdbot/scripts/tools@latest
    ```

4.  **Install dependencies and run the bot:**
    ```sh
    go build -o tgmusicbot main.go
    ```

5.  **Create a service file:**
    ```sh
    sudo nano /etc/systemd/system/tg.service
    ```

6.  **Add the following content:**
    Replace `/path/to/TgMusicBot` with the actual path to your bot directory.

    ```ini
    [Unit]
    Description=TgMusicBot Service
    After=network.target

    [Service]
    User=root
    WorkingDirectory=/root/tg
    ExecStart=/root/tg/tgmusicbot
    Restart=always
    RestartSec=900

    [Install]
    WantedBy=multi-user.target
    ```

7.  **Reload systemd and start the service:**
    ```sh
    sudo systemctl daemon-reload
    sudo systemctl start tg
    sudo systemctl enable tg
    ```

8.  **Check status and logs:**
    ```sh
    sudo systemctl status tgmusicbot
    journalctl -u tgmusicbot -f
    ```
