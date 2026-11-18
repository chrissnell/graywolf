#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>
#include "fx25.h"
#include "textcolor.h"
static unsigned char wire[300];
static int nbits, previous;
void tone_gen_put_bit(int chan, int line) {
    int bit = (line == previous); previous = line;
    if (bit) wire[nbits/8] |= 1 << (nbits%8);
    nbits++;
}
void text_color_set(dw_color_t c) {}
int dw_printf(const char *fmt, ...) { return 0; }
int main(int argc, char **argv) {
    unsigned char frame[240]; int len=0;
    if (argc != 3) return 2;
    for (char *p=argv[2]; p[0] && p[1]; p+=2) { unsigned int b; if(sscanf(p,"%2x",&b)!=1 || len>=235) return 2; frame[len++]=b; }
    fx25_init(0);
    if (fx25_send_frame(0,frame,len,100+atoi(argv[1]))<0) return 3;
    return fwrite(wire,1,nbits/8,stdout)==nbits/8 ? 0 : 4;
}
