// Configuration is read once at startup.
const { NODE_ENV, PORT = 3000 } = process.env;

export const config = {
  env: NODE_ENV,
  port: PORT,
  database: process.env.DATABASE_URL,
  redis: process.env["REDIS_URL"],

  // SESSION_SECRET is never defined anywhere in this project.
  sessionSecret: process.env.SESSION_SECRET,
};
