import { type RouteConfig, index, route } from '@react-router/dev/routes';

export default [
  index('routes/home.tsx'),
  route('login', 'routes/login.tsx'),
  route('account', 'routes/account.tsx'),
  route('chats', 'routes/chat.tsx'),
] satisfies RouteConfig;
