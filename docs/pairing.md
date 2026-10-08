# Pairing

Pairing links a bot to a WhatsApp account as a linked device. The device keys
live in the SQLite file named by `BOTKIT_STORE_PATH`. Each bot needs its own
file, so set a different path for each before pairing two bots.

Pair with a QR code:

```bash
mise exec -- bin/botkit-flow pair
```

Or request a pairing code for the account's phone number:

```bash
mise exec -- bin/botkit-flow pair --phone +51999999999
```

Use `bin/botkit-latex` for the latex bot.

On the phone, open WhatsApp, choose Settings, then Linked devices. For the QR
code, choose Link a device and scan it. For the pairing code, choose Link with
phone number and enter the printed code. The command prints `Paired as <jid>.`
and exits with status 0.

## Refusals

`pair` refuses, with exit status 1, when:

- stdin or stdout is not a terminal.
- `--phone` is not `+` followed by 7 to 15 digits.
- the store already holds a device.
- the code expires unused, or WhatsApp ends the pairing.

`run` does not pair. With no device in its store it exits with status 78. When
WhatsApp logs the device out, the store deletes the device, so `pair` works
again on the same file.
