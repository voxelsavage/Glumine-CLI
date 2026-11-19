# Glumine CLI

Create and manage local Minecraft Java Edition servers from your terminal. Glumine CLI is a standalone Go application built with Bubble Tea. It supports Vanilla, Paper, Purpur, Fabric, Quilt, Forge, and NeoForge.

## Quick start

With Go 1.25 or later installed, run:

```sh
git clone https://github.com/voxelsavage/Glumine-CLI.git
cd Glumine-CLI
go run .
```

1. Press **N** in the library and give your server a name. Choose a Minecraft release, server type, and build from the menus. For Vanilla, the build matches the game release.
2. Set the port and maximum memory. Minecraft account verification is enabled by default. Turn on **Offline mode** if you want to allow players to join without account verification.
3. Select **Choose Java** to use an existing runtime or download one automatically. Then select **Create server** to download the server package.
4. Open the new profile and press **Enter** to start it. On first start, review the Minecraft EULA and choose whether to accept. Glumine stores that choice for this server.

The interface uses Russian for Russian locales and English otherwise. At startup it checks `LC_ALL`, then `LC_MESSAGES`, then `LANG`. When those variables are unset, Windows and macOS use the system language preference. For example, on Linux you can run `LC_ALL=C ./glumine-cli` for English.

## Build

| Platform | Build | Run |
| --- | --- | --- |
| Linux | `go build -o glumine-cli .` | `./glumine-cli` |
| Windows PowerShell | `go build -o Glumine-CLI.exe .` | `.\Glumine-CLI.exe` |

GitHub Actions tests and builds the CLI on Linux x64 and Windows x64. Open a successful run and download the binary from **Artifacts**. [Build workflow](https://github.com/voxelsavage/Glumine-CLI/actions/workflows/build.yml)

## Java and server installation

**Download automatically** is the default for new profiles. On first start after EULA acceptance, Glumine downloads a compatible Eclipse Temurin runtime, verifies its SHA-256 checksum, and extracts it using the system `tar` utility. Profiles that need the same runtime share the cached installation.

**Choose Java path** lets you enter a path or browse to a JDK directory, `java`, or `java.exe`. Glumine checks the Java version when creating the profile. The `-java` flag preselects a runtime in the creation form. Older profiles without a Java preference check the flag, then `JAVA_HOME`, then `PATH`.

An internet connection is needed for version catalogs and downloads. Forge, NeoForge, and Quilt prepare their libraries on first start. The Quilt installer may need an additional Java 17 runtime for older Minecraft versions.

## Library and dashboard

In the library, **Enter** opens the selected server's console, **F2** opens its dashboard, **T** opens the trash, and **R** reloads profiles from disk. If a profile cannot be read, the library shows its folder name. Fix its `profile.json` and press **R** to reload it.

| Section | What you can do |
| --- | --- |
| Console | Start the server with **Enter**, send Minecraft commands without a leading slash, and scroll logs with **PgUp/PgDn**. Use **Ctrl+S** to stop or **Ctrl+R** to restart. |
| Files | Browse directories, create files and folders, edit text, and confirm deletions. |
| Databases | Browse SQLite files in the server folder or open an external database by path. Inspect tables and run read-only SELECT or WITH queries in pages of 100 rows. |
| Players | View online and known players, operators, bans, and the whitelist. Send moderation commands to a running server. |
| Backups | Create ZIP archives of a stopped server. Archives are stored under `backups/<server-id>` in the data directory. |
| Add-ons | Search Modrinth or, for Paper and Purpur, Hangar. Install compatible versions with checksum verification and enable or disable JARs. Install required dependencies separately. |
| Network | View local connection addresses and the server port, or look up the public IP on demand. |
| Settings | Set initial and maximum memory, edit JVM flags and Minecraft properties, check disk usage, reinstall the server software while keeping the world, or move the profile to the trash. |

Use the arrow keys to navigate, **Enter** to select, and **Esc** to go back. Long lists support **PgUp/PgDn** and **Home/End**. The trash keeps the entire profile directory until you restore or permanently delete it.

## Code editor

Open **F2 → Files**, choose a UTF-8 file, and press **Enter**. Syntax highlighting follows the filename. Wide terminals show an outline beside the document. **F4** opens the outline at any supported terminal size.

| Shortcut | Action |
| --- | --- |
| Ctrl+S / Esc | Save / close with an unsaved-changes prompt |
| Ctrl+F / F3 / Alt+P | Find / next match / previous match |
| Ctrl+R / Ctrl+G | Replace one match / go to line |
| Shift + arrows / Ctrl+A | Extend selection / select all |
| Ctrl+C / Ctrl+X / Ctrl+V | Copy / cut / paste |
| Ctrl+Z / Ctrl+Y | Undo / redo |
| Tab / Shift+Tab | Indent / unindent |
| F1 | Show editor shortcuts |

The editor supports files up to 256 KiB and preserves tabs, UTF-8 BOMs, and existing line endings. Managed files and files belonging to a running server open read-only. If another program changes an open file, saving reports a conflict and keeps your edits in the buffer.

## Data and shutdown

Profiles and worlds are stored under `Glumine-CLI/servers` in your user configuration directory. Use `-data-dir` to choose another data directory. Each directory supports one CLI instance and one running server at a time.

Press **Q** in the library or **Ctrl+C** outside the editor to quit. If a server is running, Glumine sends the Minecraft `stop` command and gives it up to 20 seconds to save the world.
