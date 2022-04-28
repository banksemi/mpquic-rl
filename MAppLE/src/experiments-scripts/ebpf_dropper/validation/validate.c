#include <stdint.h>
#include <stdio.h>
#include <string.h>

__attribute__((always_inline)) uint32_t get_random_u32(uint32_t *state)
{
    return *state = ((uint64_t)*state * 48271u) % 0x7fffffff;
}

__attribute__((always_inline)) uint32_t get_random_smaller_than(uint32_t *state, uint32_t max) {
    uint32_t r;
    int i;
    for (i = 0 ; i < 5 ; i++) {
        r = get_random_u32(state);
        if (r < (0x7fffffff - 0x7fffffff % max))
            break;
    }

    r %= max;
    return r;
}


int main() {
    uint64_t vals[10000];
    memset(vals, 0, 10000*sizeof(uint64_t));
    uint32_t seed = 43;
    uint32_t proba_times_100 = 738;
    uint64_t max = 200000000;
    uint64_t win = 0;
    for (int i = 0 ; i < max ; i++) {
        uint32_t val = get_random_smaller_than(&seed, 10000);
        vals[val]++;
        if (val < proba_times_100) win++;
    }
    for (int i = 0 ; i < 10000 ; i++) {
        printf("VALS[%d] = %lu\n", i, vals[i]);
    }
    double proba_percent = (double) proba_times_100/100.0;
    double proba = (double) proba_percent/100.0;
    printf("theoretical proba = %.4f (%.2f%%)\nempirical proba = %lu/%lu = %.4f = %.2f%%\n", proba, proba_percent, win, max, (((double)win) / max), (((double)win) / max)*100.0);
}