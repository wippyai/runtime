#include <stdio.h>
#include <errno.h>
int check(void) { FILE *f=fopen("/data/input.txt", "r"); if (!f) return errno; int c=fgetc(f); fclose(f); return c==109 ? 0 : 99; }
