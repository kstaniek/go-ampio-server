# Raspberry Pi + Waveshare 2‑CH CAN HAT (MCP2515) — Setup for `go-ampio-server`

This guide shows how to wire the **Waveshare 2‑CH CAN HAT** to a Raspberry Pi and configure **CAN0** at **50 kbps** (Ampio default), then install and run `go-ampio-server`.

> **Note:** On some Raspberry Pi OS versions the config file lives at `/boot/firmware/config.txt`. If `/boot/config.txt` doesn’t exist on your system, use `/boot/firmware/config.txt` instead.

---

## 1) Hardware hookup

1. Power off the Raspberry Pi.
2. Seat the Waveshare **2‑CH CAN HAT** on the 40‑pin header.
3. Wire your CAN bus:
   - **CANH** → bus CAN High  
   - **CANL** → bus CAN Low  
   - **GND** → common ground
4. Ensure bus termination is correct: typically **120 Ω at both ends** of the CAN segment (some HATs have a jumper to enable/disable termination).

---

## 2) Enable SPI and MCP2515 overlays

Edit the boot config:

```bash
sudo nano /boot/config.txt
# or: sudo nano /boot/firmware/config.txt
```

Append the lines below (leave any existing content intact):

```
dtparam=spi=on
dtoverlay=mcp2515-can1,oscillator=16000000,interrupt=25
dtoverlay=mcp2515-can0,oscillator=16000000,interrupt=23
dtoverlay=spi-bcm2835-overlay
```

Reboot:

```bash
sudo reboot
```

---

## 3) Bring up CAN0 at 50 kbps (Ampio)

Optionally install handy tools:

```bash
sudo apt update
sudo apt install -y can-utils
```

Bring up **can0** (one‑off for this session):

```bash
sudo ip link set can0 up type can bitrate 50000
sudo ifconfig can0 txqueuelen 65536
```

Quick check:

```bash
ip -details link show can0
# optional listen-only sniff:
# candump can0
```

> If you’ll also use **can1**, repeat with `can1` in the commands above.

---

## 4) Make CAN settings persistent (systemd‑networkd)

Create a network unit for `can0`:

```bash
sudo mkdir -p /etc/systemd/network
sudo tee /etc/systemd/network/can.network >/dev/null <<'EOF'
[Match]
Name=can0

[CAN]
BitRate=50000

[Link]
ActivationPolicy=up
EOF
```

Enable `systemd-networkd` (if not already in use on your system):

```bash
sudo systemctl enable --now systemd-networkd
```

> If your distro uses NetworkManager or `dhcpcd` instead, either keep using the manual `ip link` commands via a small oneshot systemd service at boot, or configure CAN in your chosen network stack. The file above takes effect only when `systemd-networkd` manages the link.

---

## 5) Make `txqueuelen` persistent (udev)

By default the TX queue length resets on reboot. Use a udev rule to set it automatically whenever a CAN interface appears.  
The rule below sets `txqueuelen` to **1000** (as in the example). If you prefer to match the earlier one‑off setting (**65536**), change the value accordingly.

```bash
cat <<'EOF' | sudo tee /etc/udev/rules.d/10-can-queue.rules
SUBSYSTEM=="net", ACTION=="add|change", KERNEL=="can*", ATTR{tx_queue_len}="1000"
EOF

sudo udevadm control --reload
sudo udevadm trigger --subsystem-match=net --action=change
```

Reboot and verify:

```bash
sudo reboot
# after reboot
ip link show can0
# look for: txqueuelen 1000   (or your chosen value)
```

---

## 6) Install `go-ampio-server`

1. Download the latest `.deb` from the Releases page of: `github.com/kstaniek/go-ampio/server`.
2. Install:

   ```bash
   sudo dpkg -i ./go-ampio-server_*.deb
   ```

3. Start and enable the service:

   ```bash
   sudo systemctl enable --now go-ampio-server
   systemctl status go-ampio-server
   ```

---

## 7) Troubleshooting

- Check overlays and driver bind:

  ```bash
  dmesg | grep -i -E 'mcp2515|can0|spi'
  ```

- Verify link state & bitrate:

  ```bash
  ip -details link show can0
  ```

- If the interface won’t come up, double‑check:
  - SPI is enabled and overlays are spelled exactly as above.
  - Correct **interrupt GPIOs** for your HAT (here: `23` for `can0`, `25` for `can1`).
  - Proper bus wiring and termination; try **listen‑only** to debug without driving the bus:
    ```bash
    sudo ip link set can0 down
    sudo ip link set can0 type can bitrate 50000 listen-only on
    sudo ip link set can0 up
    candump can0
    ```

---

**Done.** After reboot, `can0` should come up at **50 kbps** and `go-ampio-server` will be ready to use.