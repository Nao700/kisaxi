#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static FILE *f; static int freqset;
int rtlsdr_get_device_count(void){return 1;}
const char* rtlsdr_get_device_name(unsigned i){return "Fake RTL";}
int rtlsdr_open(void **dev, unsigned i){ f=fopen(getenv("FAKE_IQ"),"rb"); *dev=(void*)0x1234; return f?0:-1;}
int rtlsdr_close(void*d){ if(f)fclose(f); return 0;}
int rtlsdr_set_sample_rate(void*d, unsigned r){ return r==2048000?0:-1;}
int rtlsdr_set_center_freq(void*d, unsigned fr){ return fr==928900000?0:-1;}
int rtlsdr_set_freq_correction(void*d,int p){return 0;}
int rtlsdr_set_tuner_gain_mode(void*d,int m){return 0;}
int rtlsdr_set_tuner_gain(void*d,int g){return 0;}
int rtlsdr_get_tuner_gains(void*d,int*g){return 0;}
int rtlsdr_set_agc_mode(void*d,int o){return 0;}
int rtlsdr_reset_buffer(void*d){return 0;}
int rtlsdr_read_sync(void*d, void*buf, int len, int*n){ int r=fread(buf,1,len,f); *n=r; return 0;}
