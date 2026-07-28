FROM node:20-alpine

# Supplied at build time with --build-arg; nothing in this project provides it.
ARG BUILD_VERSION

ARG NODE_VERSION=20

ENV APP_HOME=/opt/app
ENV NODE_ENV=production \
    PORT=3000 \
    PATH=$APP_HOME/bin:$PATH

WORKDIR $APP_HOME
COPY . .
RUN npm ci --omit=dev

CMD ["node", "src/server.js"]
