"""Spec for the ytm-alice speaker install (fork, owner decisions E16-E20).

User story: the owner installs YouTube Music on the speaker with one script.
The fork build of ytm-alice replaces the packaged binary, the Bluetooth media
layer is left out, and the boot hook starts the player with its own voice
commands and phone casting off, restarts it after a crash, turns off its
«Включаю» phrase and lets only the bridge host reach the panel.
"""
import hashlib, io, os, pathlib, shutil, subprocess, tarfile, tempfile, time, unittest

HERE = pathlib.Path(__file__).resolve().parent


def build_arm64(dir):
    src = dir / "main.go"
    src.write_text("package main\nfunc main() {}\n")
    out = dir / "ytm-alice"
    env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="arm64")
    subprocess.run(["go", "build", "-o", str(out), str(src)], check=True, env=env, cwd=dir)
    return out


def fake_package(dir):
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w") as tar:
        def add(name, data):
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o755
            tar.addfile(info, io.BytesIO(data))
        add("MANIFEST.yaml", b"name: local/ytm-alice\ntarget: bergamot\n")
        for f in ["ytm-alice", "ytm-alice-resolver", "ytm-alice-player", "ytm-alice-player-arm64", "ytm-alice-player-armhf", "alice-media", "bt-proxy"]:
            add("files/bin/" + f, b"packaged " + f.encode())
        add("files/share/local.ytm-alice/README.md", b"readme")
    pkg = dir / "pkg.yampkg"
    pkg.write_bytes(subprocess.run(["zstd", "-q", "-c"], input=raw.getvalue(), capture_output=True, check=True).stdout)
    return pkg


class InstallTest(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        self.bin = build_arm64(self.tmp)
        self.stage = self.tmp / "out"
        env = dict(os.environ, YTM_ALICE_ROOT=str(self.tmp / "root"))
        subprocess.run(["sh", str(HERE / "install.sh"), "--dry-run", str(self.stage), str(fake_package(self.tmp)), str(self.bin), "root@speaker", "192.0.2.10"], check=True, env=env, capture_output=True)
        self.files = self.stage / "stage"

    def test_fork_binary_replaces_the_packaged_one_without_the_media_layer(self):
        got = hashlib.sha256((self.files / "bin/ytm-alice").read_bytes()).hexdigest()
        self.assertEqual(got, hashlib.sha256(self.bin.read_bytes()).hexdigest())
        names = sorted(p.name for p in (self.files / "bin").iterdir())
        self.assertEqual(names, ["ytm-alice", "ytm-alice-player", "ytm-alice-player-arm64", "ytm-alice-player-armhf", "ytm-alice-resolver"])

    def run_hook(self, action, fake_player):
        root = self.tmp / "root"
        (root / "bin").mkdir(parents=True, exist_ok=True)
        player = root / "bin/ytm-alice"
        player.write_text(fake_player)
        player.chmod(0o755)
        shim = self.tmp / "shim"
        shim.mkdir(exist_ok=True)
        calls = self.tmp / "calls"
        for ipt in ["iptables", "ip6tables"]:
            (shim / ipt).write_text(f'#!/bin/sh\necho "{ipt} $*" >> {calls}\n[ "$1" = -D ] && exit 1\nexit 0\n')
            (shim / ipt).chmod(0o755)
        run = self.tmp / "run"
        run.mkdir(exist_ok=True)
        env = dict(os.environ, PATH=f"{shim}:{os.environ['PATH']}", YTM_ALICE_RUN=str(run))
        subprocess.run(["sh", str(self.files / "S99zytm-alice"), action], check=True, env=env, timeout=10)
        return calls, run

    def test_hook_starts_the_player_without_voice_or_casting_and_guards_the_panel(self):
        env_log = self.tmp / "env"
        calls, run = self.run_hook("start", f'#!/bin/sh\n[ "$1" = serve ] && env > {env_log} && exec sleep 3\nexit 0\n')
        for _ in range(50):
            if env_log.exists():
                break
            time.sleep(0.1)
        self.addCleanup(self.run_hook, "stop", "#!/bin/sh\nexit 0\n")
        env = env_log.read_text()
        self.assertIn("YTM_ALICE_VOICE=0", env)
        self.assertIn("YTM_ALICE_CAST=0", env)
        rules = calls.read_text()
        self.assertIn("iptables -A YTM_ALICE -s 192.0.2.10 -j ACCEPT", rules)
        self.assertIn("iptables -A YTM_ALICE -j DROP", rules)
        self.assertIn("ip6tables -A YTM_ALICE -j DROP", rules)
        self.assertNotIn("ip6tables -A YTM_ALICE -s", rules)
        self.assertIn("iptables -I INPUT -p tcp --dport 24721 -j YTM_ALICE", rules)

    def test_hook_restarts_the_player_after_a_crash(self):
        starts = self.tmp / "starts"
        calls, run = self.run_hook("start", f'#!/bin/sh\n[ "$1" = serve ] && echo x >> {starts}\nexit 1\n')
        self.addCleanup(self.run_hook, "stop", "#!/bin/sh\nexit 0\n")
        deadline = time.time() + 9
        while time.time() < deadline and (not starts.exists() or len(starts.read_text().split()) < 2):
            time.sleep(0.2)
        self.assertGreaterEqual(len(starts.read_text().split()), 2, "a crashed player was not started again")

    def test_hook_turns_off_the_players_own_announcement(self):
        self.assertIn("announce-off", (self.files / "S99zytm-alice").read_text())

    def test_stop_removes_the_panel_rule(self):
        calls, _ = self.run_hook("stop", "#!/bin/sh\nexit 0\n")
        rules = calls.read_text()
        self.assertIn("iptables -X YTM_ALICE", rules)
        self.assertNotIn("-I INPUT", rules)


if __name__ == "__main__":
    unittest.main()
