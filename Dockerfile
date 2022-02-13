# Go derlenmesi için iki fazlı bir build işlemi gerçekleştireceğiz.
# Birincisi kaynak koddan exe oluşturma ikincisi ise bu exe kodunu çalıştırma şeklinde olacaktır.
FROM golang:1.17-alpine3.15 AS build-env
RUN apk add build-base

# kaynak kodlarımızı oluşturduğumuz image içine alıyoruz.
ADD . /src

# kaynak koduna gidip go build diyerek tek bir exe çıktısı alıyoruz.
RUN cd /src && go build -o goapp

# İkinci faz ise yalnızca birinci fazdaki exe klasörünü alıp çalıştırma olacaktır.
FROM alpine

# /app klasörüne gidiyoruz.
WORKDIR /app

# Exe dosyamızı birinci fazdan alıyoruz.
COPY --from=build-env /src/goapp /app/

# Go uygulamamız 9090 portundan çalışması planlanmaktadır. Bu sebeple bunu imaja bildiriyoruz.
EXPOSE 9090

# uygulamamızı çalıştırıyoruz.
ENTRYPOINT ./goapp