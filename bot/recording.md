# REPLbot session recording

This ZIP archive contains a recording of your REPLbot session.
It contains the following files:

## terminal.txt
The entire terminal output as you'd see it in your own terminal window. 
Depending on how much output you produced, this output may be cut off.

## replay.asciinema
This file can be used to replay the terminal session in realtime using
the `asciinema` tool. Simply run this command and be amazed:

```
$ asciinema play replay.asciinema
```

You may also speed up replay a little with the `-s`/`--speed` option, and avoid
long pauses with the `-i`/`--idle-time-limit` option:

```
$ asciinema play -s 1.2 -i 3 replay.asciinema
```
