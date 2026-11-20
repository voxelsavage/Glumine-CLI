# Glumine CLI

Run a local Minecraft Java Edition server from your terminal. Choose a Minecraft release and server build, let Glumine set up Java, then manage the console, world files, backups, and add-ons from one keyboard-driven interface.

## Download and run

| Platform | V1 download | Launch |
| --- | --- | --- |
| Windows x64 | [glumine-cli.exe](https://github.com/voxelsavage/Glumine-CLI/releases/download/V1/glumine-cli.exe) | Open PowerShell in the download folder and run `.\glumine-cli.exe`. |
| Linux x64 | [glumine-cli](https://github.com/voxelsavage/Glumine-CLI/releases/download/V1/glumine-cli) | In the download folder, run `chmod +x glumine-cli`, then `./glumine-cli`. |

The binaries run without a Go installation. Glumine uses Russian when it detects a Russian locale and English otherwise. To find newer versions, visit [Releases](https://github.com/voxelsavage/Glumine-CLI/releases/latest).

## Create your first server

1. Press **N** in the library and name your server.
2. Choose a Minecraft version, server type, and build. Glumine supports **Vanilla, Paper, Purpur, Fabric, Quilt, Forge, and NeoForge**. The choices depend on the Minecraft release.
3. Set the port and maximum memory. Keep **Offline mode** off to have Minecraft verify player accounts.
4. Open **Choose Java**. Select **Download automatically**, or point Glumine to an existing JDK folder or Java executable. Select **Create server** to download the server package.
5. Select the new profile and press **Enter** to open its console. Press **Enter** again to start. On first launch, review the Minecraft EULA and choose whether to accept it for this server.

Version catalogs and downloads need an internet connection. With automatic Java setup, Glumine downloads Eclipse Temurin after you accept the EULA, verifies its SHA-256 checksum, and extracts it using the system `tar` utility. Compatible profiles share the cached runtime. Forge, NeoForge, and Quilt prepare their libraries on first start. Older Quilt servers may also need Java 17 for the installer.

## Manage a server

Press **F2** on a selected profile or in its console to open the dashboard. Use the arrow keys to navigate, **Enter** to open a section, and **Esc** to go back.

| Section | What you can do |
| --- | --- |
| Console | Read logs and send Minecraft commands without a leading slash. **Ctrl+S** stops the server, **Ctrl+R** restarts it, and **PgUp/PgDn** scroll the log. |
| Files | Browse the server directory, create files and folders, and open text files in the built-in editor. |
| Databases | Inspect SQLite tables and run read-only SELECT or WITH queries. Results are paged in groups of 100 rows. |
| Players | View player lists and operators. Kick, ban, unban, or manage the whitelist on a running server. |
| Backups | Save a ZIP of a stopped server. Find the archive under `backups/<server-id>` in the data directory. |
| Add-ons | Find compatible mods and plugins on Modrinth, or Paper plugins on Hangar. Enable or disable installed JARs. Install required dependencies separately. |
| Network | See connection addresses and the server port, and look up the public IP on demand. |
| Settings | Adjust memory and JVM flags, edit Minecraft properties, check disk usage, reinstall the server software, or move the profile to the trash. |

In the library, **R** reloads profiles from disk. **T** opens the trash, where you can restore a profile with its world or permanently delete it.

### Edit a config file

Open **F2 → Files**, select a file such as `server.properties`, and press **Enter**. The editor highlights syntax and shows the document outline beside the text in wide terminals.

| Shortcut | Action |
| --- | --- |
| Ctrl+S / Esc | Save / close. Closing a modified file prompts you to save or discard your edits. |
| Ctrl+F / F3 / Alt+P | Find text / next match / previous match |
| Ctrl+R / Ctrl+G | Replace a match / jump to a line |
| Shift + arrows / Ctrl+A | Extend the selection / select all |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste |
| Ctrl+Z / Ctrl+Y | Undo / redo |
| Tab / Shift+Tab | Indent / unindent |
| F4 / F1 | Open the document outline / show editor shortcuts |

The editor supports UTF-8 files up to 256 KiB and preserves tabs, byte-order marks, and existing line endings. Managed files and files belonging to a running server open read-only. If another program changes an open file, Glumine reports the conflict and keeps your edits in the buffer.

## Storage and shutdown

Glumine stores profiles and worlds under `Glumine-CLI/servers` in your user configuration directory. Use `-data-dir` to select a different data directory or `-java` to preselect a Java executable. Each data directory supports one CLI instance and one running server at a time.

Press **Q** in the library to quit. If a server is running, Glumine sends the Minecraft `stop` command and gives it up to 20 seconds to save the world.

## Build from source

Install **Go 1.25 or later**, then run:

```sh
git clone https://github.com/voxelsavage/Glumine-CLI.git
cd Glumine-CLI
go test ./...
go run .
```

To build a standalone binary, run `go build -trimpath -o glumine-cli .` on Linux or `go build -trimpath -o glumine-cli.exe .` on Windows. On pushes and pull requests, [GitHub Actions](https://github.com/voxelsavage/Glumine-CLI/actions/workflows/build.yml) runs static checks, tests both interface languages, and builds binaries for both platforms. Download those binaries from **Artifacts** on a successful run.

## Questions and feedback

Use [Discussions](https://github.com/voxelsavage/Glumine-CLI/discussions) for setup questions and ideas. To [report a bug](https://github.com/voxelsavage/Glumine-CLI/issues), include your OS, terminal, Glumine CLI version, and the steps to reproduce it. For server launch issues, also include the Minecraft version, server type, and Java version.
