FROM node:22-alpine AS builder
WORKDIR /build
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
ARG VITE_OIDC_ISSUER_URL=/auth
ARG VITE_OIDC_CLIENT_ID=
ARG VITE_API_URL=/api
ENV VITE_OIDC_ISSUER_URL=$VITE_OIDC_ISSUER_URL
ENV VITE_OIDC_CLIENT_ID=$VITE_OIDC_CLIENT_ID
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

FROM nginx:alpine
COPY --from=builder /build/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
