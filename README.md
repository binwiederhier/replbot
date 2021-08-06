# 🤖 REPLbot
REPLbot is a Slack bot that allows you to control a REPL from within Slack. It comes with a few REPLs, but you can easiyl make/bring your own.

## Why ...?
I thought it might be a fun way to collaboratively dabble with a REPL in a team. Yes, I could have gone for a terminal in a browser, but there's nothing like having it right there in Slack. Mainly I did it because it was fun though. 😄

## How it works
I use `screen` and the `screen -X hardcopy` command to run most of the show. It's simple, but effective. In the first iteration I tried using a pseudo terminal (pty) directly, but with all the escape sequences and commands, it was getting kinda tiresome and I was spending time with stuff that I didn't want to spend time with (though I learned a lot!). And `screen` does its job so well.

## Installation
Install steps:

1. Make sure `screen` is installed. Then install REPLbot using any of the methods below. 
2. Then edit `/etc/replbot/config.yml` to add Slack bot token. The config also explains how to create a Slack app and acquire this token.
3. Review the scripts in `/etc/replbot/script.d`, and make sure that you have Docker installed if you'd like to use them.
4. If you're running REPLbot as non-root user (such as when you install the deb/rpm), be sure to add the `replbot` user to the `docker` group: `sudo usermod -G docker -a replbot`.
5. Then just run it with `replbot` (or `systemctl start replbot` when using the deb/rpm).

**Debian/Ubuntu** (*from a repository*)**:**
```bash
curl -sSL https://archive.heckel.io/apt/pubkey.txt | sudo apt-key add -
sudo apt install apt-transport-https
sudo sh -c "echo 'deb [arch=amd64] https://archive.heckel.io/apt debian main' > /etc/apt/sources.list.d/archive.heckel.io.list"  
sudo apt update
sudo apt install replbot screen
```

**Debian/Ubuntu** (*manual install*)**:**
```bash
sudo apt install screen
wget https://github.com/binwiederhier/pcopy/releases/download/v0.1.0/replbot_0.1.0_amd64.deb
dpkg -i replbot_0.1.0_amd64.deb
```

**Fedora/RHEL/CentOS:**
```bash
rpm -ivh https://github.com/binwiederhier/replbot/releases/download/v0.1.0/replbot_0.1.0_amd64.rpm
```

**Docker:**
```bash
# Be sure "screen" is installed
docker run --rm -it binwiederhier/replbot
```

**Go:**
```bash
# Be sure "screen" is installed
go get -u heckel.io/replbot
```

**Manual install** (*any x86_64-based Linux*)**:**
```bash
wget https://github.com/binwiederhier/replbot/releases/download/v0.1.0/replbot_0.1.0_linux_x86_64.tar.gz
sudo tar -C /usr/bin -zxf replbot_0.1.0_linux_x86_64.tar.gz replbot
```

## Contributing
I welcome any and all contributions. Just create a PR or an issue, or talk to me [on Slack](https://gophers.slack.com/archives/C02ABHKDCN7).

## License
Made with ❤️ by [Philipp C. Heckel](https://heckel.io), distributed under the [Apache License 2.0](LICENSE).