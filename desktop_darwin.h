#include <stddef.h>

typedef struct {
    int status;
    char *content_type;
    void *body;
    size_t size;
} DotaResponse;

void DotaRunApp(void);
void DotaShowError(const char *message);
