import os, shutil, subprocess, zipfile

PROJECT_DIR = "plex-discord-rpc"
PLUGIN_DIR = "plugin"
BUILD_DIR = "build"

TARGETS = [
    {"name": "windows", "goos": "windows", "goarch": "amd64", "binary": "plex-discord-rpc.exe"},
    {"name": "macos", "goos": "darwin", "goarch": "amd64", "binary": "plex-discord-rpc"},
    {"name": "linux", "goos": "linux", "goarch": "amd64", "binary": "plex-discord-rpc"},
]

def build_binary(target):
    print(f"Building for {target['name']}...")
    out = os.path.join(BUILD_DIR, target["name"], "scripts", target["binary"])
    env = os.environ.copy()
    env["GOOS"] = target["goos"]
    env["GOARCH"] = target["goarch"]
    result = subprocess.run(
        ["go", "build", "-o", os.path.abspath(out), "."],
        cwd=PROJECT_DIR,
        env=env
    )
    if result.returncode != 0:
        raise RuntimeError(f"Build failed for {target['name']}")
    print(f"go helper built to {out}")

def copy_plugin(target_name):
    dest = os.path.join(BUILD_DIR, target_name)
    for item in os.listdir(PLUGIN_DIR):
        src = os.path.join(PLUGIN_DIR, item)
        dst = os.path.join(dest, item)
        if os.path.isdir(src):
            shutil.copytree(src, dst, dirs_exist_ok=True)
        else:
            shutil.copy2(src, dst)
    scripts = {
        "windows": "install_windows.bat",
        "macos": "install_darwin.sh",
        "linux": "install_flatpak.sh",
    }
    script = os.path.join("install_scripts", scripts[target_name])
    if os.path.exists(script):
        shutil.copy2(script, os.path.join(dest, scripts[target_name]))
        print(f"Copied {scripts[target_name]} to {dest}")
    print(f"Copied plugin files to {dest}")

def zip_folder(target_name):
    folder = os.path.join(BUILD_DIR, target_name)
    zip_path = os.path.join(BUILD_DIR, f"{target_name}.zip")

    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for root, _, files in os.walk(folder):
            for file in files:
                full_path = os.path.join(root, file)
                arcname = os.path.relpath(full_path, folder)
                zf.write(full_path, arcname)

    print(f"created {zip_path}")

def main():
    if os.path.exists(BUILD_DIR):
        shutil.rmtree(BUILD_DIR)
    os.makedirs(BUILD_DIR)
    for target in TARGETS:
        os.makedirs(os.path.join(BUILD_DIR, target["name"]))

    for target in TARGETS:
        build_binary(target)
        copy_plugin(target["name"])
        zip_folder(target["name"])
        print(f"Done: {target['name']}")

    print("\nAll builds complete.")

if __name__ == "__main__":
    main()