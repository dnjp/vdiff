#include <u.h>
#include <libc.h>
#include <plumb.h>
#include <draw.h>
#include <event.h>
#include <keyboard.h>
#include <bio.h>

typedef struct Line Line;
typedef struct Col Col;

struct Line {
	int t;
	char *s;
	char *f;
	int l;
};

struct Col {
	Image *bg;
	Image *fg;
};

enum
{
	Lfile = 0,
	Lsep,
	Ladd,
	Ldel,
	Lnone,
	Ncols,
};

enum
{
	Scrollwidth = 12,
	Scrollgap = 2,
	Margin = 8,
	Hpadding = 4,
	Vpadding = 2,
};

Rectangle sr;
Rectangle scrollr;
Rectangle scrposr;
Rectangle listr;
Rectangle textr;
Col cols[Ncols];
Col scrlcol;
int scrollsize;
int lineh;
int nlines;
int offset;
Line **lines;
int lsize;
int lcount;
int maxlength;
int Δpan;
const char ellipsis[] = "...";

void
drawline(Rectangle r, Line *l)
{
	Point p;
	Rune  rn;
	char *s;
	int off, tab, nc;

	draw(screen, r, cols[l->t].bg, nil, ZP);
	p = Pt(r.min.x + Hpadding, r.min.y + (Dy(r)-font->height)/2);
	off = Δpan / stringwidth(font, " ");
	for(s = l->s, nc = -1, tab = 0; *s; nc++, tab--, off--){
		if(tab <= 0 && *s == '\t'){
			tab = 4 - nc % 4;
			s++;
		}
		if(tab > 0){
			if(off <= 0)
				p = runestring(screen, p, cols[l->t].bg, ZP, font, L"█");
		}else if((p.x+Hpadding+stringwidth(font, " ")+stringwidth(font, ellipsis)>=textr.max.x)){
			string(screen, p, cols[l->t].fg, ZP, font, ellipsis);
			break;
		}else{
			s += chartorune(&rn, s);
			if(off <= 0)
				p = runestringn(screen, p, cols[l->t].fg, ZP, font, &rn, 1);
		}
	}
}

void
redraw(void)
{
	Rectangle lr;
	int i, h, y;

	draw(screen, sr, cols[Lnone].bg, nil, ZP);
	draw(screen, scrollr, scrlcol.bg, nil, ZP);
	if(lcount>0){
		h = ((double)nlines/lcount)*Dy(scrollr);
		y = ((double)offset/lcount)*Dy(scrollr);
		scrposr = Rect(scrollr.min.x, scrollr.min.y+y, scrollr.max.x-1, scrollr.min.y+y+h);
	}else
		scrposr = Rect(scrollr.min.x, scrollr.min.y, scrollr.max.x-1, scrollr.max.y);
	draw(screen, scrposr, scrlcol.fg, nil, ZP);
	for(i=0; i<nlines && offset+i<lcount; i++){
		lr = Rect(textr.min.x, textr.min.y+i*lineh, textr.max.x, textr.min.y+(i+1)*lineh);
		drawline(lr, lines[offset+i]);
	}
}

void
pan(int off)
{
	int max;

	max = Hpadding + maxlength * stringwidth(font, " ") + 2 * stringwidth(font, ellipsis) - Dx(textr);
	Δpan += off * stringwidth(font, " ");
	if(Δpan < 0 || max <= 0)
		Δpan = 0;
	else if(Δpan > max)
		Δpan = max;
	redraw();
}

void
scroll(int off)
{
	if(off<0 && offset<=0)
		return;
	if(off>0 && offset+nlines>lcount)
		return;
	offset += off;
	if(offset<0)
		offset = 0;
	if(offset+nlines>lcount)
		offset = lcount-nlines+1;
	redraw();
}

int
indexat(Point p)
{
	int n;

	if (!ptinrect(p, textr))
		return -1;
	n = (p.y - textr.min.y) / lineh;
	if ((n+offset) >= lcount)
		return -1;
	return n;
}

void
eresized(int new)
{
	if(new && getwindow(display, Refnone)<0)
		sysfatal("cannot reattach: %r");
	sr = screen->r;
	scrollr = sr;
	scrollr.max.x = scrollr.min.x+Scrollwidth+Scrollgap;
	listr = sr;
	listr.min.x = scrollr.max.x;
	textr = insetrect(listr, Margin);
	lineh = Vpadding+font->height+Vpadding;
	nlines = Dy(textr)/lineh;
	scrollsize = mousescrollsize(nlines);
	if(offset > 0 && offset+nlines>lcount)
		offset = lcount-nlines+1;
	redraw();
}

void
initcol(Col *c, ulong fg, ulong bg)
{
	c->fg = allocimage(display, Rect(0,0,1,1), screen->chan, 1, fg);
	c->bg = allocimage(display, Rect(0,0,1,1), screen->chan, 1, bg);
}

void
initcols(int black)
{
	if(black){
		initcol(&scrlcol,     0x22272EFF, 0xADBAC7FF);
		initcol(&cols[Lfile], 0xADBAC7FF, 0x2D333BFF);
		initcol(&cols[Lsep],  0xADBAC7FF, 0x263549FF);
		initcol(&cols[Ladd],  0xADBAC7FF, 0x273732FF);
		initcol(&cols[Ldel],  0xADBAC7FF, 0x3F2D32FF);
		initcol(&cols[Lnone], 0xADBAC7FF, 0x22272EFF);
	}else{
		initcol(&scrlcol,     DWhite, 0x999999FF);
		initcol(&cols[Lfile], DBlack, 0xEFEFEFFF);
		initcol(&cols[Lsep],  DBlack, 0xEAFFFFFF);
		initcol(&cols[Ladd],  DBlack, 0xE6FFEDFF);
		initcol(&cols[Ldel],  DBlack, 0xFFEEF0FF);
		initcol(&cols[Lnone], DBlack, DWhite);
	}
}

int
linetype(char *text)
{
	int type;

	type = Lnone;
	if(strncmp(text, "+++", 3)==0)
		type = Lfile;
	else if(strncmp(text, "---", 3)==0){
		if(strlen(text) > 4)
			type = Lfile;
	}else if(strncmp(text, "@@", 2)==0)
		type = Lsep;
	else if(strncmp(text, "+", 1)==0)
		type = Ladd;
	else if(strncmp(text, "-", 1)==0)
		type = Ldel;
	return type;
}

Line*
parseline(char *f, int n, char *s)
{
	Line *l;
	int len;

	l = malloc(sizeof *l);
	if(l==nil)
		sysfatal("malloc: %r");
	l->t = linetype(s);
	l->s = s;
	l->l = n;
	if(l->t != Lfile && l->t != Lsep)
		l->f = f;
	else
		l->f = nil;
	len = strlen(s);
	if(len > maxlength)
		maxlength = len;
	return l;
}

int
lineno(char *s)
{
	char *p, *t[5];
	int n, l;

	p = strdup(s);
	n = tokenize(p, t, 5);
	if(n<=0)
		return -1;
	l = atoi(t[2]);
	free(p);
	return l;
}

void
parse(int fd)
{
	Biobuf *bp;
	Line *l;
	char *s, *f, *t;
	int n, ab;

	ab = 0;
	n = 0;
	f = nil;
	lsize = 64;
	lcount = 0;
	lines = malloc(lsize * sizeof *lines);
	if(lines==nil)
		sysfatal("malloc: %r");
	bp = Bfdopen(fd, OREAD);
	if(bp==nil)
		sysfatal("Bfdopen: %r");
	for(;;){
		s = Brdstr(bp, '\n', 1);
		if(s==nil)
			break;
		l = parseline(f, n, s);
		if(l->t == Lfile && l->s[0] == '-' && strncmp(l->s+4, "a/", 2)==0)
			ab = 1;
		if(l->t == Lfile && l->s[0] == '+'){
			f = l->s+4;
			if(ab && strncmp(f, "b/", 2)==0){
				f += 1;
				if(access(f, AEXIST) < 0)
					f += 1;
			}
			t = strchr(f, '\t');
			if(t!=nil)
				*t = 0;
		}else if(l->t == Lsep)
			n = lineno(l->s);
		else if(l->t == Ladd || l->t == Lnone)
			++n;
		lines[lcount++] = l;
		if(lcount>=lsize){
			lsize *= 2;
			lines = realloc(lines, lsize*sizeof *lines);
			if(lines==nil)
				sysfatal("realloc: %r");
		}
	}
}

void
plumb(char *f, int l)
{
	/*
	In plan9port libplumb depends on lib9pclient which depends on libthread.
	Just invoke plumb(1) on plan9port instead of migrating vdiff to libthread.
	*/
	pid_t pid = fork();
	if(pid==-1)
		fprint(2, "fork failed");
	else if(pid>0)
		free(wait());
	else{
		char addr[300]={0};
		char *argv[7];
		int i = 0;
		snprint(addr, sizeof addr, "%s:%d", f, l);
		argv[i++] = "plumb";
		argv[i++] = "-s"; argv[i++] = "vdiff";
		argv[i++] = "-d"; argv[i++] = "edit";
		argv[i++] = addr;
		argv[i++] = nil;
		exec("plumb", argv);
	}
}

void
usage(void)
{
	fprint(2, "%s [-b]\n", argv0);
	exits("usage");
}

void
main(int argc, char *argv[])
{
	Event ev;
	int e, n, b;

	b = 0;
	ARGBEGIN{
	case 'b':
		b = 1;
		break;
	default:
		usage();
		break;
	}ARGEND;

	parse(0);
	if(lcount==0){
		fprint(2, "no diff\n");
		exits(nil);
	}
	if(initdraw(nil, nil, "vdiff")<0)
		sysfatal("initdraw: %r");
	initcols(b);
	einit(Emouse|Ekeyboard);
	eresized(0);
	for(;;){
		e = event(&ev);
		switch(e){
		case Emouse:
			if(ptinrect(ev.mouse.xy, scrollr)){
				if(ev.mouse.buttons&1){
					n = (ev.mouse.xy.y - scrollr.min.y) / lineh;
					if(-n<lcount-offset){
						scroll(-n);
					} else {
						scroll(-lcount+offset);
					}
					break;
				}else if(ev.mouse.buttons&2){
					n = (ev.mouse.xy.y - scrollr.min.y) * lcount / Dy(scrollr);
					offset = n;
					redraw();
				}else if(ev.mouse.buttons&4){
					n = (ev.mouse.xy.y - scrollr.min.y) / lineh;
					if(n<lcount-offset){
						scroll(n);
					} else {
						scroll(lcount-offset);
					}
					break;
				}
			}
			if(ev.mouse.buttons&4){
				n = indexat(ev.mouse.xy);
				if(n>=0 && lines[n+offset]->f != nil)
					plumb(lines[n+offset]->f, lines[n+offset]->l);
			}else if(ev.mouse.buttons&8)
				scroll(-scrollsize);
			else if(ev.mouse.buttons&16)
				scroll(scrollsize);
			break;
		case Ekeyboard:
			switch(ev.kbdc){
			case 'q':
			case Kdel:
				goto End;
				break;
			case Khome:
				scroll(-1000000);
				break;
			case Kend:
				scroll(1000000);
				break;
			case Kpgup:
				scroll(-nlines);
				break;
			case Kpgdown:
				scroll(nlines);
				break;
			case Kup:
				scroll(-1);
				break;
			case Kdown:
				scroll(1);
				break;
			case Kleft:
				pan(-4);
				break;
			case Kright:
				pan(4);
				break;
			}
			break;
		}
	}
End:
	exits(nil);
}
