<?php

namespace App\Datacore\API\Writer;

use App\Datacore\API\Writer\Exception\BuilderException;
use App\Datacore\Entity\Branch;
use App\Datacore\Entity\Node;
use App\Datacore\Entity\Project;
use App\Datacore\Executor\Executor;
use App\Datacore\Executor\ListenerRegistry;
use App\Datacore\Executor\NoDiscardListener;
use App\Datacore\ExecutorInterface;
use App\Datacore\Lifecycle\History\HistoryWriter;
use App\Datacore\Lifecycle\Outbound\PublishListener;
use App\Datacore\Lifecycle\Workflow\ChangeListener;
use App\Datacore\ListenerInterface;
use App\Datacore\Reference\Address;
use Ramsey\Uuid\Uuid;
use Ramsey\Uuid\UuidInterface;
use Spiral\Prototype\Annotation\Prototyped;

/**
 * Provides the ability to simplify node and data building process, batch
 * writes (if batch size is specified).
 */
#[Prototyped(property: "builder")]
class Writer
{
    private InputQueue $queue;
    private ?Address $context = null;

    public function __construct(
        private ExecutorInterface $executor,
        private readonly ?HistoryWriter $history = null
    ) {
        $this->queue = new InputQueue();
    }

    /**
     * @return void
     */
    public function flush(): void
    {
        $this->queue->flush();
        $this->context = null;
    }

    public function withHistory(): self
    {
        if ($this->history === null) {
            throw new BuilderException("No history writer given");
        }

        $registry = clone $this->executor->getListener();
        assert($registry instanceof ListenerRegistry);

        $registry->addListener($this->history);

        $writer = clone $this;
        $writer->executor = $writer->executor->withListener($registry);

        return $writer;
    }

    public function withoutPublishing(): self
    {
        return $this->wrapListeners(static function (
            ListenerInterface $listener
        ) {
            if ($listener instanceof PublishListener) {
                return null;
            }

            return $listener;
        });
    }

    public function withoutReferenceTrigger(): self
    {
        return $this->wrapListeners(static function (
            ListenerInterface $listener
        ) {
            if ($listener instanceof ChangeListener) {
                return null;
            }

            return $listener;
        });
    }

    public function withoutErrorPublishing(): self
    {
        return $this->wrapListeners(static function (
            ListenerInterface $listener
        ) {
            if ($listener instanceof PublishListener) {
                return new NoDiscardListener($listener);
            }

            return $listener;
        });
    }

    private function wrapListeners(callable $callback): self
    {
        assert($this->executor instanceof Executor);
        $registry = clone $this->executor->getListener();

        // todo: we can encapsulate it all into single interface
        assert($registry instanceof ListenerRegistry);
        $registry->wrapListeners($callback);

        $builder = clone $this;
        $builder->executor = $builder->executor->withListener($registry);

        return $builder;
    }

    public function with(
        Address|Node|Branch|Project $path,
        bool $isolateQueue = false
    ): self {
        $builder = clone $this;
        if ($path instanceof Address) {
            $builder->context = $path;
        } elseif ($path instanceof Branch) {
            $builder->context = Address::from(branch: $path);
        } elseif ($path instanceof Project) {
            $builder->context = Address::from(project: $path);
        } else {
            $builder->context = Address::from(node: $path);
        }

        if ($isolateQueue) {
            $builder->queue = new InputQueue();
        }

        return $builder;
    }

    public function addNode(
        string $type,
        UuidInterface|null $uuid = null,
        UuidInterface|null $parent_uuid = null
    ): self {
        if ($this->context === null) {
            throw new BuilderException("Context is not set");
        }

        if ($uuid === null) {
            $uuid = Uuid::uuid7();
        }

        $node = new \App\Datacore\Operation\Node\DTO\CreateDTO(
            branch_uuid: $this->context->branch_uuid,
            uuid: $uuid,
            parent_uuid: $parent_uuid ?? $this->context->node_uuid,
            type: $type
        );
        $this->queue->push($node);

        return $this->with($this->context->withUuids(node_uuid: $uuid));
    }

    public function addReference(
        string $type,
        string $target,
        string $discriminator = "",
        bool $on_branch = false,
        UuidInterface|null $uuid = null
    ): self {
        if ($this->context->project_uuid === null) {
            throw new BuilderException("Project context is missing");
        }

        if ($uuid === null) {
            $uuid = Uuid::uuid7();
        }

        $this->queue->push(
            new \App\Datacore\Operation\Reference\DTO\CreateDTO(
                project_uuid: $this->context->project_uuid,
                branch_uuid: $this->context->branch_uuid,
                uuid: $uuid,
                type: $type,
                target: $target,
                discriminator: $discriminator,
                on_branch: $on_branch
            )
        );

        return $this;
    }

    public function updateReference(
        UuidInterface $uuid,
        string $discriminator
    ): self {
        if ($this->context->project_uuid === null) {
            throw new BuilderException("Project context is missing");
        }

        $this->queue->push(
            new \App\Datacore\Operation\Reference\DTO\UpdateDTO(
                project_uuid: $this->context->project_uuid,
                branch_uuid: $this->context->branch_uuid,
                uuid: $uuid,
                discriminator: $discriminator
            )
        );

        return $this;
    }

    public function dropReference(UuidInterface $uuid): self
    {
        if ($this->context->project_uuid === null) {
            throw new BuilderException("Project context is missing");
        }

        $this->queue->push(
            new \App\Datacore\Operation\Reference\DTO\DeleteDTO(
                project_uuid: $this->context->project_uuid,
                branch_uuid: $this->context->branch_uuid,
                uuid: $uuid
            )
        );

        return $this;
    }

    public function addProject(
        string $title,
        string $type,
        string $description = "",
        UuidInterface $parent_project_uuid = null,
        UuidInterface|null $uuid = null,
        UuidInterface|null $org_uuid = null
    ): self {
        $uuid = $uuid ?? Uuid::uuid7();

        if (
            $parent_project_uuid === null &&
            $this->context !== null &&
            $org_uuid === null
        ) {
            $parent_project_uuid = $this->context->project_uuid;
        }

        $project = new \App\Datacore\Operation\Project\DTO\CreateDTO(
            uuid: $uuid,
            title: $title,
            description: $description,
            type: $type,
            parent_uuid: $parent_project_uuid,
            organization_uuid: $org_uuid
        );

        $this->queue->push($project);

        return $this->with(Address::fromUuids(project: $uuid));
    }

    public function addBranch(
        string $title,
        string $description,
        string $type,
        UuidInterface $project_uuid = null,
        UuidInterface|null $uuid = null
    ): self {
        if ($uuid === null) {
            $uuid = Uuid::uuid7();
        }

        $branch = new \App\Datacore\Operation\Branch\DTO\CreateDTO(
            project_uuid: $project_uuid,
            uuid: $uuid,
            title: $title,
            description: $description,
            type: $type
        );
        $this->queue->push($branch);

        return $this->with(
            Address::fromUuids(branch: $uuid, project: $project_uuid)
        );
    }

    public function touchBranch(?int $ttl = null): self
    {
        if ($this->context === null) {
            throw new BuilderException("Context is not set");
        }

        if (
            $this->context->project_uuid === null ||
            $this->context->branch_uuid === null
        ) {
            throw new BuilderException("Context must have project and branch");
        }

        $this->queue->push(
            new \App\Datacore\Operation\Branch\DTO\TouchDTO(
                project_uuid: $this->context->project_uuid,
                uuid: $this->context->branch_uuid,
                cooldown_ttl: $ttl
            )
        );

        return $this;
    }

    public function updateBranch(
        UuidInterface $uuid,
        ?string $title = null,
        ?string $description = null,
        ?int $version = null
    ): self {
        $branch = new \App\Datacore\Operation\Branch\DTO\UpdateDTO(
            project_uuid: $this->context->project_uuid,
            uuid: $uuid,
            version: $version,
            title: $title,
            description: $description
        );
        $this->queue->push($branch);

        return $this;
    }

    public function addBranchMetadata(
        string $type,
        string|array|object|null $content = "",
        string $discriminator = null,
        UuidInterface|null $uuid = null
    ): self {
        if ($this->context === null) {
            throw new BuilderException("Context is not set");
        }

        if (
            $this->context->project_uuid === null ||
            $this->context->branch_uuid === null
        ) {
            throw new BuilderException("Context must have project and branch");
        }

        if ($uuid === null) {
            $uuid = Uuid::uuid7();
        }

        if (is_array($content) || is_object($content)) {
            $content = json_encode($content);
        }

        $data = new \App\Datacore\Operation\BranchMetadata\DTO\CreateDTO(
            project_uuid: $this->context->project_uuid,
            branch_uuid: $this->context->branch_uuid,
            uuid: $uuid,
            type: $type,
            content: $content ?? "",
            discriminator: $discriminator
        );

        $this->queue->push($data);

        return $this;
    }

    public function deleteNode(Node|UuidInterface $node = null): self
    {
        $branch_uuid = null;
        if ($node instanceof Node) {
            $branch_uuid = $node->branch_uuid;
        }

        if ($branch_uuid === null) {
            if ($this->context === null) {
                throw new BuilderException("Context is not set");
            }

            $branch_uuid = $this->context->branch_uuid;
        }

        if ($node instanceof Node) {
            $uuid = $node->uuid;
        } else {
            $uuid = $node;
        }

        $this->queue->push(
            new \App\Datacore\Operation\Node\DTO\DeleteDTO(
                branch_uuid: $branch_uuid,
                uuid: $uuid
            )
        );

        return $this;
    }

    public function deleteBranch(
        Branch|Address|UuidInterface $branch = null
    ): self {
        $project_uuid = null;
        if ($branch instanceof Branch) {
            $project_uuid = $branch->project_uuid;
        }

        if ($branch instanceof Address) {
            $project_uuid = $branch->project_uuid;
        }

        if ($project_uuid === null) {
            if ($this->context === null) {
                throw new BuilderException("Context is not set");
            }

            $project_uuid = $this->context->project_uuid;
        }

        if ($branch instanceof Branch) {
            $uuid = $branch->uuid;
        } elseif ($branch instanceof Address) {
            $uuid = $branch->branch_uuid;
        } else {
            $uuid = $branch;
        }

        $this->queue->push(
            new \App\Datacore\Operation\Branch\DTO\DeleteDTO(
                project_uuid: $project_uuid,
                uuid: $uuid,
                version: null
            )
        );

        return $this;
    }

    public function fork(): self
    {
        $builder = clone $this;
        $builder->queue = new InputQueue();

        return $builder;
    }

    public function add(object ...$input): self
    {
        foreach ($input as $item) {
            $this->queue->push($item);
        }

        return $this;
    }

    public function addData(
        string $type,
        string|array|object $content = "",
        int $value = 0,
        string $discriminator = null,
        UuidInterface|null $uuid = null
    ): self {
        if ($this->context === null) {
            throw new BuilderException("Context is not set");
        }

        if ($this->context->node_uuid === null) {
            throw new BuilderException("Context must be node");
        }

        if ($uuid === null) {
            $uuid = Uuid::uuid7();
        }

        if (is_array($content) || is_object($content)) {
            $content = json_encode($content);
        }

        $data = new \App\Datacore\Operation\Data\DTO\CreateDTO(
            branch_uuid: $this->context->branch_uuid,
            node_uuid: $this->context->node_uuid,
            uuid: $uuid,
            type: $type,
            content: $content,
            value: $value,
            discriminator: $discriminator
        );

        $this->queue->push($data);

        return $this;
    }

    public function typecastNode(
        UuidInterface $uuid,
        ?string $type = null
    ): self {
        $this->queue->push(
            new \App\Datacore\Operation\Node\DTO\UpdateDTO(
                branch_uuid: $this->context->branch_uuid,
                uuid: $uuid,
                type: $type
            )
        );

        return $this;
    }

    public function upsertData(
        string $type,
        null|string|array|object $content = "",
        null|int $value = 0,
        null|string $discriminator = null,
        ?UuidInterface $uuid = null
    ): self {
        if (
            $this->context?->node_uuid === null ||
            $this->context?->branch_uuid === null
        ) {
            throw new BuilderException("Context must be qualified node");
        }

        if (is_array($content) || is_object($content)) {
            $content = json_encode($content);
        }

        $data = new \App\Datacore\Operation\Data\DTO\UpsertDTO(
            branch_uuid: $this->context->branch_uuid,
            node_uuid: $this->context->node_uuid,
            type: $type,
            content: $content,
            value: $value,
            discriminator: $discriminator,
            uuid: $uuid
        );

        $this->queue->push($data);

        return $this;
    }

    public function deleteData(
        ?string $type = null,
        ?UuidInterface $uuid = null,
        ?string $discriminator = null
    ): self {
        if ($this->context->node_uuid === null) {
            throw new BuilderException("Context node is not set");
        }

        if ($uuid === null && $type === null && $discriminator === null) {
            throw new BuilderException(
                "Either type, uuid or discriminator must be specified"
            );
        }

        $op = new \App\Datacore\Operation\Data\DTO\DeleteDTO(
            branch_uuid: $this->context->branch_uuid,
            node_uuid: $this->context->node_uuid,
            uuid: $uuid,
            type: $type,
            discriminator: $discriminator,
            version: null
        );

        $this->queue->push($op);

        return $this;
    }

    public function setAccess(
        string $role,
        bool $read,
        bool $write,
        bool $execute
    ): self {
        if ($this->context?->project_uuid === null) {
            throw new BuilderException("Context must be qualified project");
        }

        $data = new \App\Datacore\Operation\Access\DTO\UpsertDTO(
            project_uuid: $this->context->project_uuid,
            branch_uuid: $this->context->branch_uuid,
            role: $role,
            write: $write,
            read: $read,
            execute: $execute
        );

        $this->queue->push($data);

        return $this;
    }

    public function deleteAccess(?string $role = null): self
    {
        if ($this->context?->project_uuid === null) {
            throw new BuilderException("Context must be qualified project");
        }

        $op = new \App\Datacore\Operation\Access\DTO\DeleteDTO(
            project_uuid: $this->context->project_uuid,
            branch_uuid: $this->context->branch_uuid,
            role: $role
        );

        $this->queue->push($op);

        return $this;
    }

    public function deleteBranchMetadata(?UuidInterface $uuid = null): self
    {
        if (
            $this->context?->branch_uuid === null ||
            $this->context?->project_uuid === null
        ) {
            throw new BuilderException(
                "Context must point to branch and project"
            );
        }

        $this->queue->push(
            new \App\Datacore\Operation\BranchMetadata\DTO\DeleteDTO(
                project_uuid: $this->context->project_uuid,
                branch_uuid: $this->context->branch_uuid,
                uuid: $uuid,
                version: null
            )
        );

        return $this;
    }

    public function upsertBranchMetadata(
        string $type,
        string|array|object|null $content = "",
        ?string $discriminator = null
    ): self {
        if (
            $this->context?->branch_uuid === null ||
            $this->context?->project_uuid === null
        ) {
            throw new BuilderException(
                "Context must point to branch and project"
            );
        }

        if (is_array($content) || is_object($content)) {
            $content = json_encode($content);
        }

        $data = new \App\Datacore\Operation\BranchMetadata\DTO\UpsertDTO(
            project_uuid: $this->context->project_uuid,
            branch_uuid: $this->context->branch_uuid,
            type: $type,
            content: $content,
            discriminator: $discriminator
        );

        $this->queue->push($data);

        return $this;
    }

    public function upsertProjectMetadata(
        string $type,
        string|array|object $content = "",
        string $discriminator = null,
        UuidInterface|null $uuid = null
    ): self {
        if ($this->context?->project_uuid === null) {
            throw new BuilderException("Context must point to project");
        }

        if (is_array($content) || is_object($content)) {
            $content = json_encode($content);
        }

        $data = new \App\Datacore\Operation\ProjectMetadata\DTO\UpsertDTO(
            project_uuid: $this->context->project_uuid,
            type: $type,
            content: $content,
            discriminator: $discriminator
        );

        $this->queue->push($data);

        return $this;
    }

    /**
     * @param UuidInterface $org_uuid
     * @param UuidInterface $uuid
     * @param string $title
     * @param string $type
     * @return $this
     */
    public function createProject(
        UuidInterface $org_uuid,
        UuidInterface $uuid,
        UuidInterface $parent_uuid,
        string $title,
        string $description = "",
        string $type = "default"
    ): self {
        $operation = new \App\Datacore\Operation\Project\DTO\CreateDTO(
            uuid: $uuid,
            title: $title,
            description: $description,
            type: $type,
            parent_uuid: $parent_uuid,
            organization_uuid: $org_uuid
        );

        $this->queue->push($operation);

        return $this;
    }

    /**
     * Deletes project and all nested branches and nodes.
     *
     * @param UuidInterface $uuid
     * @return $this
     */
    public function deleteProject(UuidInterface $uuid): self
    {
        $operation = new \App\Datacore\Operation\Project\DTO\DeleteDTO($uuid);

        $this->queue->push($operation);

        return $this;
    }

    /**
     * @throws \App\Datacore\Exception\ExecuteException
     */
    public function run(int $batch = 0): void
    {
        try {
            foreach ($this->queue->generateBatch($batch) as $batch) {
                $this->executor->execute(...$batch);
            }
        } finally {
            $this->flush();
        }
    }

    public function empty(): bool
    {
        return count($this->queue) === 0;
    }
}
