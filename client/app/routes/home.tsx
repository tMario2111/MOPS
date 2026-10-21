export function meta() {
  return [{ title: 'Chat App' }, { name: 'description', content: 'Sign in to Chat App' }];
}

export default function Home() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-6 py-12 text-slate-900">
      <form className="w-full max-w-sm space-y-6 rounded-xl border border-slate-200 bg-white p-8 shadow-sm">
        <h1 className="text-2xl font-semibold tracking-tight">Chat App</h1>

        <div className="space-y-4">
          <label className="block text-sm font-medium text-slate-700">
            Email
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="email"
              type="email"
              autoComplete="email"
              required
            />
          </label>

          <label className="block text-sm font-medium text-slate-700">
            Password
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="password"
              type="password"
              autoComplete="current-password"
              required
            />
          </label>
        </div>
      </form>
    </main>
  );
}
