<div style="text-align:center">
  <img src="https://raw.githubusercontent.com/dnjp/vdiff/master/resources/screenshot.png" alt="drawing" />
</div>

# vdiff

This is a fork of [phil9's vdiff](https://shithub.us/phil9/vdiff/HEAD/info.html)
which is intended for use on plan9port.


## Installation

vdiff can be installed just like all other software for plan9, however
`objtype` is not set in plan9port so you must set it yourself:

```
% objtype=arm64 mk install
install o.vdiff /Users/daniel/bin/arm64/vdiff
```

## Usage

A git/diff output viewer.
Right-clicking a diff line will open the file at the given line in the editor

Usage: git diff | vdiff
