/* Offline adapter to the unmodified pinned Dire Wolf Bell 202 tone generator.
 * stdin: LSB-first wire bytes; stdout: mono 48000 Hz s16le PCM.
 * No audio device or PTT access. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include "audio.h"
#include "gen_tone.h"
#include "textcolor.h"
int audio_put(int device, int byte) { return fputc(byte,stdout)==EOF ? -1 : 0; }
void text_color_set(dw_color_t c) {}
int dw_printf(const char *fmt, ...) { return 0; }
int main(int argc, char **argv) {
    int hf = argc > 1 && atoi(argv[1]) == 300;
    struct audio_s config; memset(&config,0,sizeof(config));
    config.adev[0].defined=1;
    config.adev[0].num_channels=1;
    config.adev[0].samples_per_sec=48000;
    config.adev[0].bits_per_sample=16;
    config.chan_medium[0]=MEDIUM_RADIO;
    config.achan[0].modem_type=MODEM_AFSK;
    config.achan[0].baud=hf ? 300 : 1200;
    config.achan[0].mark_freq=hf ? 1600 : 1200;
    config.achan[0].space_freq=hf ? 1800 : 2200;
    gen_tone_init(&config,50,1);
    int byte, line=0;
    while((byte=getchar())!=EOF) for(int n=0;n<8;n++) {
        if (!(byte & (1<<n))) line=!line;
        tone_gen_put_bit(0,line);
    }
    return ferror(stdout) ? 1 : 0;
}
