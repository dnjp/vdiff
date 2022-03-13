<$PLAN9/src/mkhdr

# objtype is no longer set in p9p - set this yourself.
BIN=$home/bin/$objtype
CFLAGS=-FTVw
TARG=vdiff
OFILES=vdiff.$O

<$PLAN9/src/mkone

upstream:V:
	git remote add upstream git://shithub.us/phil9/vdiff
